// Package subprocess provides a config-declared Renderer that shells out to an
// arbitrary command. It lets users plug in tools like `kustomize build` or
// `helm template` from their workspace config without recompiling yayamlls;
// the compiled-in flate renderer is the built-in equivalent.
package subprocess

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/home-operations/yayamlls/internal/render"
)

// Config is the `renderers:` entry shape for a subprocess renderer.
//
//	renderers:
//	  kustomize:
//	    match: { kind: Kustomization, group: kustomize.toolkit.fluxcd.io }
//	    command: ["kustomize", "build", "{dir}"]
type Config struct {
	Enabled *bool     `json:"enabled,omitempty"`
	Match   MatchRule `json:"match"`
	Command []string  `json:"command"`
}

// MatchRule selects which documents a renderer handles. Group matches on a
// group boundary, so "helm.toolkit.fluxcd.io" covers every version of it.
type MatchRule struct {
	Kind  string `json:"kind"`
	Group string `json:"group,omitempty"`
}

// FromConfig is a render.Factory: it builds a subprocess Renderer from a
// config entry. ok is false when the entry isn't a subprocess renderer (no
// command, or no kind to match), so the registry can ignore it.
func FromConfig(name string, raw json.RawMessage) (render.Renderer, bool) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, false
	}
	if len(cfg.Command) == 0 || cfg.Match.Kind == "" {
		return nil, false
	}
	enabled := cfg.Enabled == nil || *cfg.Enabled
	return &Renderer{
		name:    name,
		enabled: enabled,
		command: cfg.Command,
		kind:    cfg.Match.Kind,
		group:   cfg.Match.Group,
	}, true
}

type Renderer struct {
	name    string
	command []string
	kind    string
	group   string

	mu      sync.Mutex
	enabled bool
	wsRoot  string
}

func (r *Renderer) Name() string { return r.name }

func (r *Renderer) IsEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled
}

func (r *Renderer) SetWorkspaceRoot(root string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wsRoot = root
}

func (r *Renderer) Matches(doc *render.SourceDocument) bool {
	return render.MatchesKind(doc, r.kind, r.group)
}

func (r *Renderer) Render(ctx context.Context, doc *render.SourceDocument) (*render.RenderedOutput, error) {
	if doc == nil {
		return &render.RenderedOutput{Provider: r.name}, nil
	}
	args := r.expand(doc)
	// A command that needs an on-disk path can't run for an unsaved buffer;
	// skip quietly rather than redline the document.
	if args == nil {
		return &render.RenderedOutput{Provider: r.name}, nil
	}
	bin, err := exec.LookPath(args[0])
	if err != nil {
		return &render.RenderedOutput{Provider: r.name}, fmt.Errorf(
			"%w: %q not found on PATH", render.ErrRendererUnavailable, args[0])
	}

	cmd := exec.CommandContext(ctx, bin, args[1:]...)
	if doc.Path != "" {
		cmd.Dir = filepath.Dir(doc.Path)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	out := &render.RenderedOutput{
		Provider: r.name,
		Raw:      append([]byte(nil), stdout.Bytes()...),
		Stderr:   append([]byte(nil), stderr.Bytes()...),
	}
	if runErr != nil {
		return out, fmt.Errorf("%s: %w (stderr: %s)", r.name, runErr, truncate(stderr.String(), 512))
	}
	manifests, err := render.ParseManifests(stdout.Bytes())
	if err != nil {
		return out, fmt.Errorf("%s: parse output: %w", r.name, err)
	}
	out.Manifests = manifests
	return out, nil
}

// expand substitutes placeholders in the configured argv. It returns nil when
// a path placeholder is required but the document has no on-disk path.
func (r *Renderer) expand(doc *render.SourceDocument) []string {
	dir, file := "", doc.Path
	if doc.Path != "" {
		dir = filepath.Dir(doc.Path)
	}
	repl := map[string]string{"{dir}": dir, "{file}": file, "{name}": doc.Name}

	out := make([]string, len(r.command))
	for i, arg := range r.command {
		if (strings.Contains(arg, "{dir}") || strings.Contains(arg, "{file}")) && doc.Path == "" {
			return nil
		}
		for ph, val := range repl {
			arg = strings.ReplaceAll(arg, ph, val)
		}
		out[i] = arg
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
