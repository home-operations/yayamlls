package lsp

import (
	"errors"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/parser"
	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestRenderDiagnostics_UnavailableRendererIsSilent(t *testing.T) {
	diags := renderDiagnostics(schema.NewStore(), schema.NewResolver(), nil,
		render.ErrRendererUnavailable, diagnostics.Options{})
	if diags != nil {
		t.Errorf("expected no diagnostics for unavailable renderer, got %v", diags)
	}
}

func TestRenderDiagnostics_RenderErrorSurfacedWithProviderSource(t *testing.T) {
	out := &render.RenderedOutput{Provider: "flate"}
	diags := renderDiagnostics(schema.NewStore(), schema.NewResolver(), out,
		errors.New("boom"), diagnostics.Options{})
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %d", len(diags))
	}
	if !strings.Contains(diags[0].Message, "boom") {
		t.Errorf("message = %q, want it to contain the render error", diags[0].Message)
	}
	if diags[0].Source == nil || *diags[0].Source != "yayamlls/flate" {
		t.Errorf("source = %v, want yayamlls/flate", diags[0].Source)
	}
}

func TestRenderSource(t *testing.T) {
	if got := renderSource(nil); got != "yayamlls/render" {
		t.Errorf("nil output: got %q, want yayamlls/render", got)
	}
	if got := renderSource(&render.RenderedOutput{}); got != "yayamlls/render" {
		t.Errorf("empty provider: got %q, want yayamlls/render", got)
	}
	if got := renderSource(&render.RenderedOutput{Provider: "flate"}); got != "yayamlls/flate" {
		t.Errorf("named provider: got %q, want yayamlls/flate", got)
	}
}

// renderedPatternError validates a rendered manifest whose hostname carries a
// Flux substitution against a pattern-constrained schema and returns the
// resulting manifest and validation error.
func renderedPatternError(t *testing.T) (render.RenderedManifest, *jsonschema.ValidationError) {
	t.Helper()
	const schemaBody = `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"hostname": {"type": "string", "pattern": "^[a-z.]+$"}
		}
	}`
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(schemaBody))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("mem://t.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("mem://t.json")
	if err != nil {
		t.Fatal(err)
	}

	f, err := parser.ParseBytes([]byte("hostname: ${EDGE_HOST}\n"), 0)
	if err != nil {
		t.Fatal(err)
	}
	m := render.RenderedManifest{
		AST:  f.Docs[0],
		GVK:  render.GVK{Version: "v1", Kind: "Pod"},
		Name: "test",
	}
	value, err := yamlast.Decode(m.AST)
	if err != nil {
		t.Fatal(err)
	}
	var verr *jsonschema.ValidationError
	if err := sch.Validate(value); !errors.As(err, &verr) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	return m, verr
}

func TestFlattenRendered_FluxSubstitutionSuppressed(t *testing.T) {
	m, verr := renderedPatternError(t)
	out := &render.RenderedOutput{Provider: "flate"}

	diags := flattenRendered(out, m, verr, diagnostics.Options{FluxSubstitutions: true})
	if len(diags) != 0 {
		t.Errorf("expected pattern error on ${...} to be suppressed, got %v", diags)
	}

	diags = flattenRendered(out, m, verr, diagnostics.Options{})
	if len(diags) != 1 {
		t.Fatalf("expected the pattern error without FluxSubstitutions, got %d", len(diags))
	}
	// Rendered docs have no source line: kind/name/pointer ride in the message.
	if !strings.Contains(diags[0].Message, "[rendered Pod/test @ /hostname]") {
		t.Errorf("message = %q, want the rendered kind/name/pointer prefix", diags[0].Message)
	}
}
