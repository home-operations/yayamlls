package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadFile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".yayamlls.yaml")
	body := `schemas:
  "./schemas/local.json":
    - "k8s/**/*.yaml"
catalog: false
renderers:
  flate:
    enabled: false
    binary: /opt/bin/flate
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if want := []string{"k8s/**/*.yaml"}; len(got.Schemas) != 1 || got.Schemas["./schemas/local.json"][0] != want[0] {
		t.Errorf("schemas: %+v", got.Schemas)
	}
	if got.Catalog == nil || *got.Catalog != false {
		t.Errorf("catalog: %+v", got.Catalog)
	}
	if _, ok := got.Renderers["flate"]; !ok {
		t.Errorf("renderers missing flate: %+v", got.Renderers)
	}
}

func TestLoadFile_AbsentReturnsZero(t *testing.T) {
	got, err := LoadFile(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Errorf("absent file should not error: %v", err)
	}
	if len(got.Schemas) != 0 || len(got.Renderers) != 0 {
		t.Errorf("expected zero-value, got %+v", got)
	}
}

func TestMerge_OverrideWins(t *testing.T) {
	base := Settings{
		Schemas:   map[string][]string{"a.json": {"a"}},
		Catalog:   new(true),
		Renderers: map[string]json.RawMessage{"flate": json.RawMessage(`{"binary":"x"}`)},
	}
	override := Settings{
		Schemas:   map[string][]string{"b.json": {"b"}},
		Catalog:   new(false),
		Renderers: map[string]json.RawMessage{"flate": json.RawMessage(`{"binary":"y"}`)},
	}
	got := Merge(base, override)
	if got.Schemas["a.json"][0] != "a" || got.Schemas["b.json"][0] != "b" {
		t.Errorf("schemas merge wrong: %+v", got.Schemas)
	}
	if *got.Catalog != false {
		t.Errorf("override catalog should win: %v", *got.Catalog)
	}
	if string(got.Renderers["flate"]) != `{"binary":"y"}` {
		t.Errorf("override renderer should win: %s", got.Renderers["flate"])
	}
}

func TestMerge_UnionsGlobsForSameSchema(t *testing.T) {
	base := Settings{Schemas: map[string][]string{"k8s.json": {"a/**", "b/**"}}}
	override := Settings{Schemas: map[string][]string{"k8s.json": {"b/**", "c/**"}}}
	got := Merge(base, override)
	want := []string{"a/**", "b/**", "c/**"}
	if g := got.Schemas["k8s.json"]; !slices.Equal(g, want) {
		t.Errorf("globs union wrong: got %v want %v", g, want)
	}
	// base must not be mutated by the merge.
	if g := base.Schemas["k8s.json"]; !slices.Equal(g, []string{"a/**", "b/**"}) {
		t.Errorf("base mutated: %v", g)
	}
}

func TestMerge_CarriesKubernetes(t *testing.T) {
	base := Settings{}
	override := Settings{Kubernetes: &KubernetesSettings{SchemaURL: "https://mirror.example/{kindLower}.json"}}
	got := Merge(base, override)
	if got.Kubernetes == nil || got.Kubernetes.SchemaURL != "https://mirror.example/{kindLower}.json" {
		t.Errorf("override kubernetes.schemaUrl dropped: %+v", got.Kubernetes)
	}
}

func TestMerge_KubernetesFieldMerge(t *testing.T) {
	// A workspace opt-out must survive an override that only sets schemaUrl.
	base := Settings{Kubernetes: &KubernetesSettings{Enabled: new(false)}}
	override := Settings{Kubernetes: &KubernetesSettings{SchemaURL: "https://mirror/{kindLower}.json"}}
	got := Merge(base, override)
	if got.Kubernetes.Enabled == nil || *got.Kubernetes.Enabled {
		t.Errorf("enabled:false dropped by partial override: %+v", got.Kubernetes)
	}
	if got.Kubernetes.SchemaURL != "https://mirror/{kindLower}.json" {
		t.Errorf("schemaUrl not merged: %+v", got.Kubernetes)
	}
	// Merge must not mutate base's pointee.
	if base.Kubernetes.SchemaURL != "" {
		t.Errorf("Merge mutated base: %+v", base.Kubernetes)
	}
}

func TestKubernetesEnabled(t *testing.T) {
	cases := []struct {
		name string
		s    *Settings
		want bool
	}{
		{"nil settings", nil, true},
		{"no block", &Settings{}, true},
		{"block, enabled unset", &Settings{Kubernetes: &KubernetesSettings{SchemaURL: "x"}}, true},
		{"enabled true", &Settings{Kubernetes: &KubernetesSettings{Enabled: new(true)}}, true},
		{"enabled false", &Settings{Kubernetes: &KubernetesSettings{Enabled: new(false)}}, false},
		{"enabled false with schemaUrl", &Settings{Kubernetes: &KubernetesSettings{Enabled: new(false), SchemaURL: "x"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.KubernetesEnabled(); got != tc.want {
				t.Errorf("KubernetesEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
func TestMerge_CarriesRenderDebounce(t *testing.T) {
	ms := 1500
	base := Settings{}
	override := Settings{RenderDebounceMs: &ms}
	got := Merge(base, override)
	if got.RenderDebounceMs == nil || *got.RenderDebounceMs != 1500 {
		t.Errorf("override renderDebounceMs dropped: %+v", got.RenderDebounceMs)
	}
	// An override that omits the field must not clear a base value.
	keep := Merge(Settings{RenderDebounceMs: &ms}, Settings{})
	if keep.RenderDebounceMs == nil || *keep.RenderDebounceMs != 1500 {
		t.Errorf("base renderDebounceMs cleared by empty override: %+v", keep.RenderDebounceMs)
	}
}

func TestMerge_CarriesRenderTimeout(t *testing.T) {
	ms := 60000
	base := Settings{}
	override := Settings{RenderTimeoutMs: &ms}
	got := Merge(base, override)
	if got.RenderTimeoutMs == nil || *got.RenderTimeoutMs != 60000 {
		t.Errorf("override renderTimeoutMs dropped: %+v", got.RenderTimeoutMs)
	}
	// An override that omits the field must not clear a base value.
	keep := Merge(Settings{RenderTimeoutMs: &ms}, Settings{})
	if keep.RenderTimeoutMs == nil || *keep.RenderTimeoutMs != 60000 {
		t.Errorf("base renderTimeoutMs cleared by empty override: %+v", keep.RenderTimeoutMs)
	}
}
