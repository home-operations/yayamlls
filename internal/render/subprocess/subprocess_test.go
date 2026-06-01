package subprocess_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/render/subprocess"
)

func TestFromConfig_RejectsIncomplete(t *testing.T) {
	cases := map[string]string{
		"no command": `{"match":{"kind":"Kustomization"}}`,
		"no kind":    `{"command":["kustomize","build"]}`,
		"malformed":  `{`,
	}
	for name, raw := range cases {
		if _, ok := subprocess.FromConfig("x", json.RawMessage(raw)); ok {
			t.Errorf("%s: expected ok=false", name)
		}
	}
}

func TestFromConfig_BuildsAndMatches(t *testing.T) {
	raw := json.RawMessage(`{
		"match": {"kind": "Kustomization", "group": "kustomize.toolkit.fluxcd.io"},
		"command": ["kustomize", "build", "{dir}"]
	}`)
	r, ok := subprocess.FromConfig("kustomize", raw)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if r.Name() != "kustomize" {
		t.Errorf("name = %q", r.Name())
	}
	doc := render.AnalyzeDocument("file:///k.yaml", "/k.yaml",
		"apiVersion: kustomize.toolkit.fluxcd.io/v1\nkind: Kustomization\nmetadata:\n  name: app\n")
	if !r.Matches(doc) {
		t.Error("expected Kustomization to match")
	}
	other := render.AnalyzeDocument("file:///c.yaml", "/c.yaml",
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n")
	if r.Matches(other) {
		t.Error("ConfigMap should not match")
	}
}

func TestRender_RunsCommandAndParses(t *testing.T) {
	dir := t.TempDir()
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: rendered\n"
	path := filepath.Join(dir, "out.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	r, ok := subprocess.FromConfig("cat", json.RawMessage(
		`{"match":{"kind":"ConfigMap"},"command":["cat","{file}"]}`))
	if !ok {
		t.Fatal("build failed")
	}
	doc := &render.SourceDocument{Path: path, Name: "rendered", Kind: "ConfigMap"}
	out, err := r.Render(context.Background(), doc)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out.Manifests) != 1 || out.Manifests[0].GVK.Kind != "ConfigMap" {
		t.Fatalf("expected 1 ConfigMap manifest, got %+v", out.Manifests)
	}
	if out.Manifests[0].Name != "rendered" {
		t.Errorf("manifest name = %q", out.Manifests[0].Name)
	}
}

func TestRender_MissingBinaryIsUnavailable(t *testing.T) {
	r, _ := subprocess.FromConfig("ghost", json.RawMessage(
		`{"match":{"kind":"ConfigMap"},"command":["yayamlls-no-such-binary-xyz","{file}"]}`))
	doc := &render.SourceDocument{Path: "/tmp/x.yaml", Kind: "ConfigMap"}
	_, err := r.Render(context.Background(), doc)
	if !errors.Is(err, render.ErrRendererUnavailable) {
		t.Errorf("expected ErrRendererUnavailable, got %v", err)
	}
}

func TestRender_SkipsUnsavedBufferNeedingPath(t *testing.T) {
	r, _ := subprocess.FromConfig("cat", json.RawMessage(
		`{"match":{"kind":"ConfigMap"},"command":["cat","{file}"]}`))
	doc := &render.SourceDocument{Path: "", Kind: "ConfigMap"} // no on-disk path
	out, err := r.Render(context.Background(), doc)
	if err != nil {
		t.Fatalf("expected silent skip, got error: %v", err)
	}
	if len(out.Manifests) != 0 {
		t.Errorf("expected no manifests, got %+v", out.Manifests)
	}
}

func TestRender_Disabled(t *testing.T) {
	r, _ := subprocess.FromConfig("cat", json.RawMessage(
		`{"enabled":false,"match":{"kind":"ConfigMap"},"command":["cat","{file}"]}`))
	if r.(interface{ IsEnabled() bool }).IsEnabled() {
		t.Error("expected disabled renderer")
	}
}
