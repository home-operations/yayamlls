package render_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/home-operations/yayamlls/internal/render"
)

// kindRenderer is a minimal Renderer for registry tests, matching by exact Kind.
type kindRenderer struct {
	name   string
	kind   string
	wsRoot string
}

func (f *kindRenderer) Name() string { return f.name }
func (f *kindRenderer) Matches(doc *render.SourceDocument) bool {
	return doc != nil && doc.Kind == f.kind
}
func (f *kindRenderer) Render(context.Context, *render.SourceDocument) (*render.RenderedOutput, error) {
	return &render.RenderedOutput{Provider: f.name}, nil
}
func (f *kindRenderer) SetWorkspaceRoot(root string) { f.wsRoot = root }

func kindFactory(name string, raw json.RawMessage) (render.Renderer, bool) {
	var cfg struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.Kind == "" {
		return nil, false
	}
	return &kindRenderer{name: name, kind: cfg.Kind}, true
}

func TestRegistry_FactoryBuildsDynamicRenderer(t *testing.T) {
	reg := render.NewRegistry()
	reg.SetFactory(kindFactory)
	reg.Configure(map[string]json.RawMessage{
		"kustomize": json.RawMessage(`{"kind":"Kustomization"}`),
	})
	got := reg.For(&render.SourceDocument{Kind: "Kustomization"})
	if got == nil || got.Name() != "kustomize" {
		t.Fatalf("expected dynamic kustomize renderer, got %v", got)
	}
}

func TestRegistry_DynamicOverridesBuiltin(t *testing.T) {
	reg := render.NewRegistry()
	reg.Register(&kindRenderer{name: "builtin", kind: "Kustomization"})
	reg.SetFactory(kindFactory)
	reg.Configure(map[string]json.RawMessage{
		"user": json.RawMessage(`{"kind":"Kustomization"}`),
	})
	got := reg.For(&render.SourceDocument{Kind: "Kustomization"})
	if got == nil || got.Name() != "user" {
		t.Fatalf("config renderer should win over built-in, got %v", got)
	}
}

func TestRegistry_ConfigureDropsRemovedEntries(t *testing.T) {
	reg := render.NewRegistry()
	reg.SetFactory(kindFactory)
	reg.Configure(map[string]json.RawMessage{"k": json.RawMessage(`{"kind":"Kustomization"}`)})
	reg.Configure(map[string]json.RawMessage{}) // entry removed
	if got := reg.For(&render.SourceDocument{Kind: "Kustomization"}); got != nil {
		t.Fatalf("expected renderer dropped, got %v", got)
	}
}

func TestRegistry_WorkspaceRootReachesDynamicRenderer(t *testing.T) {
	reg := render.NewRegistry()
	reg.SetFactory(kindFactory)
	reg.SetWorkspaceRoot("/work")
	reg.Configure(map[string]json.RawMessage{"k": json.RawMessage(`{"kind":"Kustomization"}`)})
	got := reg.For(&render.SourceDocument{Kind: "Kustomization"})
	if fr, ok := got.(*kindRenderer); !ok || fr.wsRoot != "/work" {
		t.Fatalf("dynamic renderer missing workspace root: %+v", got)
	}
}
