package lint_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/parser"
	"github.com/home-operations/yayamlls/internal/lint"
	"github.com/home-operations/yayamlls/internal/render"
)

// fakeRenderer stands in for flate: it matches a synthetic kind and returns a
// caller-supplied output/error, so the CLI's render wiring can be exercised
// offline without git or helm.
type fakeRenderer struct {
	out *render.RenderedOutput
	err error
}

func (fakeRenderer) Name() string                          { return "flate" }
func (fakeRenderer) Matches(d *render.SourceDocument) bool { return d.Kind == "FakeKind" }
func (f fakeRenderer) Render(_ context.Context, _ *render.SourceDocument) (*render.RenderedOutput, error) {
	return f.out, f.err
}

// renderWorkspace writes a workspace that resolves the synthetic FakeKind to a
// local file:// schema, so both raw and rendered validation stay offline.
func renderWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	const schema = `{
	  "$schema": "https://json-schema.org/draft/2020-12/schema",
	  "type": "object",
	  "properties": { "spec": { "type": "object",
	    "properties": { "replicas": { "type": "integer" } } } }
	}`
	mustWrite(t, filepath.Join(root, "fakekind.json"), schema)
	mustWrite(t, filepath.Join(root, ".yayamlls.yaml"),
		"catalog: false\nkubernetes:\n  schemaUrl: \"file://"+filepath.Join(root, "{kindLower}.json")+"\"\n")
	return root
}

func runReg(reg *render.Registry, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := lint.Run(args, reg, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRun_RenderFailureWarnsButDoesNotFail(t *testing.T) {
	root := renderWorkspace(t)
	doc := filepath.Join(root, "hr.yaml")
	mustWrite(t, doc, "apiVersion: fake.example.com/v1\nkind: FakeKind\nmetadata:\n  name: x\nspec:\n  replicas: 1\n")

	reg := render.NewRegistry()
	reg.Register(fakeRenderer{out: &render.RenderedOutput{Provider: "flate"}, err: errors.New("no HelmRelease named \"x\"")})

	code, stdout, _ := runReg(reg, "--root", root, "--render", doc)
	if code != 0 {
		t.Errorf("a render failure must not fail the run, got exit %d (stdout=%q)", code, stdout)
	}
	if !strings.Contains(stdout, "no HelmRelease named") {
		t.Errorf("expected the render failure to be reported, got: %q", stdout)
	}
}

func TestRun_RenderedSchemaViolationFails(t *testing.T) {
	root := renderWorkspace(t)
	doc := filepath.Join(root, "hr.yaml")
	// The source document is valid; the *rendered* manifest is not.
	mustWrite(t, doc, "apiVersion: fake.example.com/v1\nkind: FakeKind\nmetadata:\n  name: x\nspec:\n  replicas: 1\n")

	f, err := parser.ParseBytes([]byte("apiVersion: fake.example.com/v1\nkind: FakeKind\nspec:\n  replicas: \"oops\"\n"), 0)
	if err != nil {
		t.Fatal(err)
	}
	out := &render.RenderedOutput{Provider: "flate", Manifests: []render.RenderedManifest{{
		AST:  f.Docs[0],
		GVK:  render.GVK{Group: "fake.example.com", Version: "v1", Kind: "FakeKind"},
		Name: "x",
	}}}
	reg := render.NewRegistry()
	reg.Register(fakeRenderer{out: out})

	code, stdout, _ := runReg(reg, "--root", root, "--render", doc)
	if code != 1 {
		t.Errorf("a rendered schema violation must fail the run, got exit %d (stdout=%q)", code, stdout)
	}
	if !strings.Contains(stdout, "[rendered FakeKind/x") {
		t.Errorf("expected a rendered diagnostic, got: %q", stdout)
	}

	// Without --render the same files are clean.
	if code, _, _ := runReg(reg, "--root", root, doc); code != 0 {
		t.Errorf("without --render the run must be clean, got exit %d", code)
	}
}
