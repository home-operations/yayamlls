package render

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"

	yaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
	"github.com/home-operations/yayamlls/internal/yamlast"
)

// ParseManifests splits a renderer's multi-document YAML output into typed
// manifests, skipping documents with no kind.
func ParseManifests(stdout []byte) ([]RenderedManifest, error) {
	if len(strings.TrimSpace(string(stdout))) == 0 {
		return nil, nil
	}
	f, err := parser.ParseBytes(stdout, 0)
	if err != nil {
		return nil, err
	}
	out := make([]RenderedManifest, 0, len(f.Docs))
	for _, d := range f.Docs {
		if d.Body == nil {
			continue
		}
		var head struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
			Metadata   struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}
		if err := yaml.NodeToValue(d.Body, &head); err != nil {
			continue
		}
		if head.Kind == "" {
			continue
		}
		group, version := splitAPIVersion(head.APIVersion)
		out = append(out, RenderedManifest{
			AST:  d,
			GVK:  GVK{Group: group, Version: version, Kind: head.Kind},
			Name: head.Metadata.Name,
		})
	}
	return out, nil
}

// ErrRendererUnavailable signals that a renderer's external tool is not
// installed. Callers surface no diagnostic for it: a missing optional
// helper is a non-condition, not an error in the user's document.
var ErrRendererUnavailable = errors.New("renderer unavailable")

type Configurable interface {
	Configure(raw json.RawMessage) error
}

type Enableable interface {
	IsEnabled() bool
}

// WorkspaceAware renderers anchor relative config paths at the workspace root.
type WorkspaceAware interface {
	SetWorkspaceRoot(root string)
}

// Factory builds a renderer from a config entry the registry doesn't already
// know by name. It returns ok=false when the entry isn't one it can build
// (e.g. config for a compiled-in renderer, or a malformed entry).
type Factory func(name string, raw json.RawMessage) (Renderer, bool)

type Registry struct {
	mu        sync.RWMutex
	providers []Renderer // compiled-in renderers (e.g. flate)
	dynamic   []Renderer // built from config via factory, rebuilt on Configure
	factory   Factory
	wsRoot    string
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Register(p Renderer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = append(r.providers, p)
}

// SetFactory installs the builder used to materialise config-declared
// renderers. Without one, only compiled-in renderers participate.
func (r *Registry) SetFactory(f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factory = f
}

// For returns the first enabled renderer that matches doc. Config-declared
// renderers are consulted before compiled-in ones, so a user can override a
// built-in for a given kind from their workspace config.
func (r *Registry) For(doc *SourceDocument) Renderer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range append(append([]Renderer(nil), r.dynamic...), r.providers...) {
		if en, ok := p.(Enableable); ok && !en.IsEnabled() {
			continue
		}
		if p.Matches(doc) {
			return p
		}
	}
	return nil
}

// Configure applies each config entry: entries naming a compiled-in renderer
// configure it in place; the rest are rebuilt into the dynamic set via the
// factory, so removing an entry drops its renderer.
func (r *Registry) Configure(configs map[string]json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	known := make(map[string]bool, len(r.providers))
	for _, p := range r.providers {
		known[p.Name()] = true
		if raw, ok := configs[p.Name()]; ok {
			if c, ok := p.(Configurable); ok {
				_ = c.Configure(raw)
			}
		}
	}

	r.dynamic = r.dynamic[:0]
	if r.factory == nil {
		return
	}
	for name, raw := range configs {
		if known[name] {
			continue
		}
		p, ok := r.factory(name, raw)
		if !ok {
			continue
		}
		if w, ok := p.(WorkspaceAware); ok && r.wsRoot != "" {
			w.SetWorkspaceRoot(r.wsRoot)
		}
		r.dynamic = append(r.dynamic, p)
	}
}

// SetWorkspaceRoot forwards a filesystem path (not a URI) to every
// WorkspaceAware renderer and retains it for renderers built later.
func (r *Registry) SetWorkspaceRoot(root string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wsRoot = root
	for _, p := range append(append([]Renderer(nil), r.providers...), r.dynamic...) {
		if w, ok := p.(WorkspaceAware); ok {
			w.SetWorkspaceRoot(root)
		}
	}
}

func (r *Registry) All() []Renderer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append(append([]Renderer(nil), r.providers...), r.dynamic...)
}

func AnalyzeDocument(uri, path, text string) *SourceDocument {
	parsed := yamlast.Parse([]byte(text))
	if parsed.File == nil || len(parsed.File.Docs) == 0 {
		return nil
	}
	doc := parsed.File.Docs[0]
	if doc.Body == nil {
		return nil
	}
	var head struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	if err := yaml.NodeToValue(doc.Body, &head); err != nil {
		return nil
	}
	if head.Kind == "" {
		return nil
	}
	group, version := splitAPIVersion(head.APIVersion)
	return &SourceDocument{
		URI:      uri,
		Path:     path,
		Text:     text,
		AST:      parsed.File,
		Kind:     head.Kind,
		APIGroup: group + versionSep(group, version),
		Name:     head.Metadata.Name,
	}
}

func versionSep(group, version string) string {
	if group == "" {
		return version
	}
	if version == "" {
		return ""
	}
	return "/" + version
}

func splitAPIVersion(v string) (group, version string) {
	if v == "" {
		return "", ""
	}
	if !strings.Contains(v, "/") {
		return "", v
	}
	g, ver, _ := strings.Cut(v, "/")
	return g, ver
}

// MatchesKind matches doc.Kind exactly and doc.APIGroup on a group boundary
// so "helm.toolkit.fluxcd.io" matches v2beta1/v2beta2/v2 (the version follows
// a "/") but not an unrelated group that merely shares the prefix, e.g.
// "helm.toolkit.fluxcd.iox".
func MatchesKind(doc *SourceDocument, kind, group string) bool {
	if doc == nil {
		return false
	}
	if doc.Kind != kind {
		return false
	}
	return doc.APIGroup == group || strings.HasPrefix(doc.APIGroup, group+"/")
}
