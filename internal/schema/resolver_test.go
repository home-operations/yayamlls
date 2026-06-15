package schema

import (
	"testing"

	"github.com/home-operations/yayamlls/internal/config"
)

func TestResolver_ModelineBeatsSettings(t *testing.T) {
	r := NewResolver()
	r.SetSettings(config.Settings{
		Schemas: map[string][]string{"./glob.json": {"**/*.yaml"}},
		Catalog: new(false),
	})

	got := r.Resolve("# yaml-language-server: $schema=./modeline.json\nx: 1\n", "/tmp/a.yaml")
	if got != "./modeline.json" {
		t.Errorf("modeline lost to settings: %q", got)
	}
}

func TestResolver_SettingsGlobMatch(t *testing.T) {
	r := NewResolver()
	r.SetSettings(config.Settings{
		Schemas: map[string][]string{"./k8s.json": {"k8s/**/*.yaml"}},
		Catalog: new(false),
	})

	got := r.Resolve("apiVersion: v1\n", "/repo/k8s/dev/app.yaml")
	if got != "./k8s.json" {
		t.Errorf("expected ./k8s.json, got %q", got)
	}

	if r.Resolve("x: 1\n", "/repo/other/file.yaml") != "" {
		t.Errorf("expected empty for non-matching path")
	}
}

func TestResolver_K8sURLGatedByEnabled(t *testing.T) {
	gvk := GVK{Group: "apps", Version: "v1", Kind: "Deployment"}
	r := NewResolver()

	// Default (no block) is enabled.
	r.SetSettings(config.Settings{})
	if got := r.K8sURL(gvk); got == "" {
		t.Errorf("expected a URL by default, got empty")
	}

	r.SetSettings(config.Settings{Kubernetes: &config.KubernetesSettings{Enabled: new(false)}})
	if got := r.K8sURL(gvk); got != "" {
		t.Errorf("expected empty when kubernetes.enabled is false, got %q", got)
	}
}

func TestMatchSettings_DeterministicMostSpecific(t *testing.T) {
	schemas := map[string][]string{
		"./a.json": {"**/*.yaml"},
		"./b.json": {"k8s/**/*.yaml"},
		"./c.json": {"**/app.yaml"},
	}
	// Overlapping globs from three refs all match this path; the narrowest
	// (k8s-anchored) must win, identically on every call.
	const docPath = "/repo/k8s/app.yaml"
	first := matchSettings(schemas, docPath)
	if first != "./b.json" {
		t.Fatalf("matchSettings = %q, want ./b.json (most specific)", first)
	}
	for i := 0; i < 200; i++ {
		if got := matchSettings(schemas, docPath); got != first {
			t.Fatalf("nondeterministic: call %d returned %q, want %q", i, got, first)
		}
	}
}
