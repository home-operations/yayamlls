// Package subprocess provides a config-declared Renderer that shells out to an
// arbitrary command. It lets users plug in tools like `kustomize build` or
// `helm template` from their workspace config without recompiling yayamlls;
// the compiled-in flate renderer is the built-in equivalent.
package subprocess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/home-operations/yayamlls/internal/render"
)

// subprocessWaitDelay bounds how long cmd.Wait can block reading the child's
// stdout/stderr pipes after a cancellation. Without it, a wrapper that
// backgrounds a long-lived helper inheriting those pipes would keep the
// renderer goroutine and FDs alive past the render timeout. A few seconds is
// enough for well-behaved tools (kustomize, helm template) to exit; anything
// still running is killed.
const subprocessWaitDelay = 2 * time.Second

const (
	maxStdoutBytes = 64 << 20
	maxStderrBytes = 1 << 20
)

var errOutputTooLarge = errors.New("output too large")

// capWriter buffers up to max bytes. Past that, Write calls onOver and fails,
// or discards the excess when onOver is nil.
type capWriter struct {
	buf    bytes.Buffer
	max    int
	over   bool
	onOver func()
}

func (w *capWriter) Write(p []byte) (int, error) {
	room := w.max - w.buf.Len()
	if len(p) <= room {
		return w.buf.Write(p)
	}
	w.buf.Write(p[:room])
	w.over = true
	if w.onOver == nil {
		return len(p), nil
	}
	w.onOver()
	return room, errOutputTooLarge
}

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
	// Resolve a relative command against the directory it runs in.
	if doc.Path != "" && !filepath.IsAbs(args[0]) && strings.ContainsRune(args[0], filepath.Separator) {
		args[0] = filepath.Join(filepath.Dir(doc.Path), args[0])
	}
	bin, err := exec.LookPath(args[0])
	if err != nil {
		return &render.RenderedOutput{Provider: r.name}, fmt.Errorf(
			"%w: %q not found on PATH", render.ErrRendererUnavailable, args[0])
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args[1:]...)
	isolateProcessGroup(cmd)
	// Force-close the pipes if cancellation happens but grandchildren still
	// hold them, so cmd.Wait doesn't keep this goroutine (and the render
	// FDs) alive past the render timeout.
	cmd.WaitDelay = subprocessWaitDelay
	if doc.Path != "" {
		cmd.Dir = filepath.Dir(doc.Path)
	}
	stdout := &capWriter{max: maxStdoutBytes, onOver: cancel}
	stderr := &capWriter{max: maxStderrBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	out := &render.RenderedOutput{
		Provider: r.name,
		Raw:      append([]byte(nil), stdout.buf.Bytes()...),
		Stderr:   append([]byte(nil), stderr.buf.Bytes()...),
	}
	if stdout.over {
		return out, fmt.Errorf("%s: %w: stdout exceeds %d bytes", r.name, errOutputTooLarge, stdout.max)
	}
	// A cancelled or timed-out run surfaces as "signal: killed" or ErrWaitDelay;
	// report the context error so it isn't mistaken for a tool failure.
	if runErr != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, fmt.Errorf("%s: %w (stderr: %s)", r.name, runErr, truncate(stderr.buf.String(), 512))
	}
	manifests, err := render.ParseManifests(stdout.buf.Bytes())
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
