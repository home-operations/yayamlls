// Package flate is the Renderer adapter for home-operations/flate.
package flate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/home-operations/yayamlls/internal/render"
)

const providerName = "flate"

type Renderer struct {
	Binary string

	mu       sync.Mutex
	disabled bool
	resolved string // cached exec.LookPath result for the current Binary
	looked   bool   // whether resolution has been attempted for current Binary
	root     string // configured build path (--path); empty = build the file's own dir
	wsRoot   string // workspace root, to anchor a relative root
}

func New() *Renderer { return &Renderer{Binary: providerName} }

type fileConfig struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Binary  string `json:"binary,omitempty"`
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
	if cfg.Binary != "" && cfg.Binary != r.Binary {
		r.Binary = cfg.Binary
		r.resolved = ""
		r.looked = false
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
	sub, err := subcommandFor(doc)
	if err != nil {
		return nil, err
	}
	args, err := r.buildArgs(sub, doc)
	if err != nil {
		return nil, err
	}
	if args == nil {
		// No name to scope to; skip rather than render the whole tree.
		return &render.RenderedOutput{Provider: r.Name()}, nil
	}
	bin, err := r.resolveBinary()
	if err != nil {
		return &render.RenderedOutput{Provider: r.Name()}, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	out := &render.RenderedOutput{
		Provider: r.Name(),
		Raw:      append([]byte(nil), stdout.Bytes()...),
		Stderr:   append([]byte(nil), stderr.Bytes()...),
	}
	if runErr != nil {
		return out, fmt.Errorf("flate %s: %w (stderr: %s)", sub, runErr, truncate(stderr.String(), 512))
	}
	manifests, err := render.ParseManifests(stdout.Bytes())
	if err != nil {
		return out, fmt.Errorf("flate %s: parse output: %w", sub, err)
	}
	out.Manifests = manifests
	return out, nil
}

func (r *Renderer) resolveBinary() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.looked {
		name := r.Binary
		if name == "" {
			name = providerName
		}
		if abs, err := exec.LookPath(name); err == nil {
			r.resolved = abs
		}
		r.looked = true
	}
	if r.resolved == "" {
		return "", fmt.Errorf("%w: flate binary not found on PATH — install with "+
			"`go install github.com/home-operations/flate/cmd/flate@latest`", render.ErrRendererUnavailable)
	}
	return r.resolved, nil
}

func subcommandFor(doc *render.SourceDocument) (sub string, err error) {
	if doc == nil {
		return "", errors.New("nil doc")
	}
	switch {
	case render.MatchesKind(doc, "HelmRelease", "helm.toolkit.fluxcd.io"):
		return "hr", nil
	case render.MatchesKind(doc, "Kustomization", "kustomize.toolkit.fluxcd.io"):
		return "ks", nil
	default:
		return "", fmt.Errorf("flate: unsupported kind %q", doc.Kind)
	}
}

// buildArgs assembles the flate argv. A configured build path scans from that
// root and selects the resource by metadata.name, so a source defined in
// another directory resolves; without it, the document's own directory builds.
// A nil result means skip: a root is set but no name is known yet.
func (r *Renderer) buildArgs(sub string, doc *render.SourceDocument) ([]string, error) {
	if root := r.targetRoot(); root != "" {
		if doc.Name == "" {
			return nil, nil
		}
		return []string{"build", sub, doc.Name, "--path", root, "-o", "yaml"}, nil
	}
	if doc.Path == "" {
		return nil, errors.New("flate needs an on-disk file path")
	}
	return []string{"build", sub, "--path", filepath.Dir(doc.Path), "-o", "yaml"}, nil
}

// targetRoot resolves the configured build path; a relative one anchors at the
// workspace root. Empty when no path is configured.
func (r *Renderer) targetRoot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.root == "":
		return ""
	case filepath.IsAbs(r.root), r.wsRoot == "":
		return r.root
	default:
		return filepath.Join(r.wsRoot, r.root)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
