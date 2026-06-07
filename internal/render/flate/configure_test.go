package flate_test

import (
	"encoding/json"
	"testing"

	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/render/flate"
)

func TestFlate_Configure_Disables(t *testing.T) {
	r := flate.New()
	if !r.IsEnabled() {
		t.Fatalf("default should be enabled")
	}
	if err := r.Configure(json.RawMessage(`{"enabled": false}`)); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if r.IsEnabled() {
		t.Errorf("expected disabled after configure")
	}
}

func TestRegistry_For_SkipsDisabledRenderer(t *testing.T) {
	r := flate.New()
	_ = r.Configure(json.RawMessage(`{"enabled": false}`))

	reg := render.NewRegistry()
	reg.Register(r)
	doc := &render.SourceDocument{Kind: "HelmRelease", APIGroup: "helm.toolkit.fluxcd.io/v2"}
	if got := reg.For(doc); got != nil {
		t.Errorf("expected nil (disabled), got %v", got.Name())
	}
}
