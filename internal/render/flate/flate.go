// Package flate is the Renderer adapter for home-operations/flate,
// embedded as a library via its pkg/orchestrator API.
package flate

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	yaml "github.com/goccy/go-yaml"
	"github.com/home-operations/flate/pkg/helm"
	"github.com/home-operations/flate/pkg/manifest"
	"github.com/home-operations/flate/pkg/orchestrator"
	"github.com/home-operations/flate/pkg/source"
	"github.com/home-operations/flate/pkg/store"

	"github.com/home-operations/yayamlls/internal/render"
)

const providerName = "flate"

type Renderer struct {
	mu       sync.Mutex
	disabled bool
	root     string // configured build path (--path equivalent); empty = workspace root
	wsRoot   string // workspace root, to anchor a relative root

	// treeMu serializes whole-tree renders and guards cached: a blocked
	// caller is served from the cache once the in-flight render finishes.
	treeMu sync.Mutex
	cached *treeRender
}

func New() *Renderer { return &Renderer{} }

type fileConfig struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Path    string `json:"path,omitempty"`
}

func (r *Renderer) Configure(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var cfg fileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cfg.Enabled != nil {
		r.disabled = !*cfg.Enabled
	}
	r.root = cfg.Path
	return nil
}

func (r *Renderer) SetWorkspaceRoot(root string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wsRoot = root
}

func (r *Renderer) IsEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.disabled
}

func (r *Renderer) Name() string { return providerName }

func (r *Renderer) Matches(doc *render.SourceDocument) bool {
	return render.MatchesKind(doc, "HelmRelease", "helm.toolkit.fluxcd.io") ||
		render.MatchesKind(doc, "Kustomization", "kustomize.toolkit.fluxcd.io")
}

func (r *Renderer) Render(ctx context.Context, doc *render.SourceDocument) (*render.RenderedOutput, error) {
	kind, err := kindFor(doc)
	if err != nil {
		return nil, err
	}
	if doc.Name == "" {
		// No name to scope to; skip rather than render the whole tree.
		return &render.RenderedOutput{Provider: r.Name()}, nil
	}
	root := r.targetRoot()
	if root == "" {
		return nil, errors.New("flate needs a configured path or workspace root")
	}
	tree, err := r.tree(ctx, root)
	if err != nil {
		return &render.RenderedOutput{Provider: r.Name()}, fmt.Errorf("flate: %w", err)
	}
	out := &render.RenderedOutput{Provider: r.Name()}
	// Walk every loaded object of this kind so a name that didn't render
	// (failed reconcile, suspended, no docs) still counts as a match —
	// mirroring flate's own `build hr/ks <name>` scoping.
	matched := 0
	var docs []map[string]any
	var failures []string
	for _, id := range tree.byKind[kind] {
		if id.Name != doc.Name {
			continue
		}
		matched++
		if info, ok := tree.failed[id]; ok {
			failures = append(failures, id.String()+": "+info.Message)
		}
		mans := tree.manifests[id]
		if len(mans) == 0 {
			continue
		}
		// Clone-and-sort per-artifact so output is byte-stable across runs
		// (Go map iteration inside helm charts randomizes emit order).
		sorted := slices.Clone(mans)
		slices.SortStableFunc(sorted, compareDocs)
		docs = append(docs, sorted...)
	}
	if matched == 0 {
		// A name that matches nothing in the tree should error rather than
		// silently render empty — a typo shouldn't look like a clean build.
		return out, fmt.Errorf("flate: no %s named %q under %s", kind, doc.Name, root)
	}
	if len(docs) > 0 {
		raw, err := marshalDocs(docs)
		if err != nil {
			return out, fmt.Errorf("flate: encode rendered output: %w", err)
		}
		out.Raw = raw
		manifests, err := render.ParseManifests(raw)
		if err != nil {
			return out, fmt.Errorf("flate: parse output: %w", err)
		}
		out.Manifests = manifests
	}
	if len(failures) > 0 {
		slices.Sort(failures)
		return out, fmt.Errorf("flate: %s", strings.Join(failures, "; "))
	}
	return out, nil
}

// treeRender is one whole-tree orchestrator run, shared by every document
// rendered against the same root until the tree changes on disk.
type treeRender struct {
	root        string
	fingerprint string
	byKind      map[string][]manifest.NamedResource
	manifests   map[manifest.NamedResource][]map[string]any
	failed      map[manifest.NamedResource]store.StatusInfo
	err         error // bootstrap-level failure: the run produced no result
}

// tree returns the cached whole-tree render for root, rebuilding it when the
// tree's on-disk fingerprint changed. Remote source contents (git/OCI) can
// drift without a local change; like flate's own disk caches, a workspace
// file save is what triggers a re-render.
func (r *Renderer) tree(ctx context.Context, root string) (*treeRender, error) {
	fp, err := fingerprintTree(root)
	if err != nil {
		return nil, err
	}
	r.treeMu.Lock()
	defer r.treeMu.Unlock()
	if c := r.cached; c != nil && c.root == root && c.fingerprint == fp {
		return c, c.err
	}
	t := buildTree(ctx, root)
	// Never cache an interrupted render: a Run-phase cancellation still
	// yields a Result, but one holding a partial tree.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	t.root, t.fingerprint = root, fp
	r.cached = t
	return t, t.err
}

// buildTree runs one orchestrator render over root, mirroring the defaults
// `flate build` applies (skip CRDs/Secrets, shallow git clones, bounded
// source retries, default cache sizes).
func buildTree(ctx context.Context, root string) *treeRender {
	o, err := orchestrator.New(orchestrator.Config{
		Path:        root,
		WipeSecrets: true,
		HelmOptions: helm.Options{
			SkipCRDs:    true,
			SkipSecrets: true,
			KubeVersion: helm.BundledKubeVersion(),
		},
		Concurrency: runtime.NumCPU() * 4,
		SourceRetry: source.RetryConfig{
			Attempts: 3,
			MinWait:  200 * time.Millisecond,
			MaxWait:  3 * time.Second,
			Jitter:   0.1,
		},
		GitDepth:               1,
		HelmTemplateCacheBytes: helm.DefaultTemplateCacheBytes,
		HelmRenderCacheBytes:   helm.DefaultRenderCacheBytes,
	})
	if err != nil {
		return &treeRender{err: err}
	}
	defer o.Stop()
	res, runErr := o.Render(ctx)
	if res == nil {
		// Bootstrap failed: nothing rendered, surface the run error for
		// every document until the tree changes.
		return &treeRender{err: runErr}
	}
	// Per-resource Run failures live in res.Failed and are scoped to the
	// matching document at Render time; the aggregate runErr would tar
	// every open document with every failure in the tree.
	t := &treeRender{
		byKind:    map[string][]manifest.NamedResource{},
		manifests: res.Manifests,
		failed:    res.Failed,
	}
	for _, kind := range []string{manifest.KindHelmRelease, manifest.KindKustomization} {
		objs := o.Store().ListObjects(kind)
		ids := make([]manifest.NamedResource, 0, len(objs))
		for _, obj := range objs {
			ids = append(ids, obj.Named())
		}
		t.byKind[kind] = ids
	}
	return t
}

// fingerprintTree hashes the (path, size, mtime) of every file under root,
// skipping .git. Any workspace change — including files only referenced by
// kustomizations or in-repo charts — invalidates the cached render.
func fingerprintTree(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// hash.Hash writes never fail.
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00%d\n", path, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func kindFor(doc *render.SourceDocument) (string, error) {
	if doc == nil {
		return "", errors.New("nil doc")
	}
	switch {
	case render.MatchesKind(doc, "HelmRelease", "helm.toolkit.fluxcd.io"):
		return manifest.KindHelmRelease, nil
	case render.MatchesKind(doc, "Kustomization", "kustomize.toolkit.fluxcd.io"):
		return manifest.KindKustomization, nil
	default:
		return "", fmt.Errorf("flate: unsupported kind %q", doc.Kind)
	}
}

// targetRoot resolves the Flux entry path flate builds from: the configured
// path (a relative one anchors at the workspace root), falling back to the
// workspace root itself. Empty only when neither is set.
func (r *Renderer) targetRoot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.root == "":
		return r.wsRoot
	case filepath.IsAbs(r.root), r.wsRoot == "":
		return r.root
	default:
		return filepath.Join(r.wsRoot, r.root)
	}
}

// marshalDocs renders docs as multi-document YAML, the same shape the
// `flate build -o yaml` subprocess used to emit on stdout.
func marshalDocs(docs []map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	for i, doc := range docs {
		if i > 0 {
			buf.WriteString("---\n")
		}
		b, err := yaml.Marshal(doc)
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	return buf.Bytes(), nil
}

// compareDocs orders rendered docs by (kind, namespace, name).
func compareDocs(a, b map[string]any) int {
	an, ans := manifest.DocMetadata(a)
	bn, bns := manifest.DocMetadata(b)
	return cmp.Or(
		cmp.Compare(manifest.DocKind(a), manifest.DocKind(b)),
		cmp.Compare(ans, bns),
		cmp.Compare(an, bn),
	)
}
