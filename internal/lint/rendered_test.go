package lint

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
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestRenderDiagnostics_UnavailableRendererIsSilent(t *testing.T) {
	diags := RenderedDiagnostics(schema.NewStore(), schema.NewResolver(), nil, nil,
		render.ErrRendererUnavailable, diagnostics.Options{}, protocol.DiagnosticSeverityError)
	if diags != nil {
		t.Errorf("expected no diagnostics for unavailable renderer, got %v", diags)
	}
}

func TestRenderDiagnostics_RenderErrorSurfacedWithProviderSource(t *testing.T) {
	out := &render.RenderedOutput{Provider: "flate"}
	diags := RenderedDiagnostics(schema.NewStore(), schema.NewResolver(), nil, out,
		errors.New("boom"), diagnostics.Options{}, protocol.DiagnosticSeverityWarning)
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %d", len(diags))
	}
	if !strings.Contains(diags[0].Message, "boom") {
		t.Errorf("message = %q, want it to contain the render error", diags[0].Message)
	}
	if diags[0].Source == nil || *diags[0].Source != "yayamlls/flate" {
		t.Errorf("source = %v, want yayamlls/flate", diags[0].Source)
	}
	if diags[0].Severity == nil || *diags[0].Severity != protocol.DiagnosticSeverityWarning {
		t.Errorf("severity = %v, want the requested failure severity", diags[0].Severity)
	}
}

const helmReleaseSrc = `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: eventrouter
spec:
  values:
    controllers:
      main:
        automountServiceAccountToken: false
    redis:
      auth: nope
`

func helmValuesErr(body string) error {
	return errors.New("flate: HelmRelease/monitoring/eventrouter: helm template monitoring/eventrouter: " +
		"values don't meet the specifications of the schema(s) in the following chart(s):\n" + body)
}

func TestRenderedDiagnostics_HelmValuesViolations(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		body     string
		wantMsg  []string
		wantLine []uint32
	}{
		{
			name:     "unknown key anchors on the key",
			src:      helmReleaseSrc,
			body:     "app-template:\n- at '/controllers/main/automountServiceAccountToken': false schema\n",
			wantMsg:  []string{"helm values at '/controllers/main/automountServiceAccountToken': false schema"},
			wantLine: []uint32{8},
		},
		{
			name:     "missing value anchors on deepest existing key",
			src:      helmReleaseSrc,
			body:     "app-template:\n- at '/defaultPodOptions': got null, want object\n",
			wantMsg:  []string{"helm values at '/defaultPodOptions': got null, want object"},
			wantLine: []uint32{5},
		},
		{
			name: "subchart pointers are relative to its values key",
			src:  helmReleaseSrc,
			body: "app-template:\n- at '': missing property 'x'\nredis:\n- at '/auth': got string, want object\n",
			wantMsg: []string{
				"helm values at '': missing property 'x'",
				"helm values at '/redis/auth': got string, want object",
			},
			wantLine: []uint32{5, 10},
		},
		{
			name:     "nested causes are each reported",
			src:      helmReleaseSrc,
			body:     "app-template:\n- at '/controllers/main': oneOf failed\n  - at '/controllers/main': got object, want string\n",
			wantMsg:  []string{"helm values at '/controllers/main': oneOf failed", "helm values at '/controllers/main': got object, want string"},
			wantLine: []uint32{7, 7},
		},
		{
			name:     "no spec.values stays at the document start",
			src:      "apiVersion: helm.toolkit.fluxcd.io/v2\nkind: HelmRelease\nmetadata:\n  name: eventrouter\n",
			body:     "app-template:\n- at '/a': false schema\n",
			wantMsg:  []string{"helm values at '/a': false schema"},
			wantLine: []uint32{0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &render.RenderedOutput{Provider: "flate"}
			diags := RenderedDiagnostics(schema.NewStore(), schema.NewResolver(), yamlast.Parse([]byte(tt.src)), out,
				helmValuesErr(tt.body), diagnostics.Options{}, protocol.DiagnosticSeverityWarning)
			if len(diags) != len(tt.wantMsg) {
				t.Fatalf("got %d diagnostics, want %d: %+v", len(diags), len(tt.wantMsg), diags)
			}
			for i, d := range diags {
				if d.Message != tt.wantMsg[i] {
					t.Errorf("diag %d message = %q, want %q", i, d.Message, tt.wantMsg[i])
				}
				if d.Range.Start.Line != tt.wantLine[i] {
					t.Errorf("diag %d line = %d, want %d", i, d.Range.Start.Line, tt.wantLine[i])
				}
				if d.Severity == nil || *d.Severity != protocol.DiagnosticSeverityError {
					t.Errorf("diag %d severity = %v, want error", i, d.Severity)
				}
			}
		})
	}
}

func TestRenderedDiagnostics_HelmValuesJoinedFailures(t *testing.T) {
	src := yamlast.Parse([]byte(helmReleaseSrc))
	err := helmValuesErr("app-template:\n- at '/controllers/main/automountServiceAccountToken': false schema\n" +
		"; HelmRelease/other/eventrouter: source not found")
	diags := RenderedDiagnostics(schema.NewStore(), schema.NewResolver(), src, &render.RenderedOutput{Provider: "flate"},
		err, diagnostics.Options{}, protocol.DiagnosticSeverityWarning)
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want the violation plus the unrelated failure: %+v", len(diags), diags)
	}
	if diags[0].Range.Start.Line != 8 || *diags[0].Severity != protocol.DiagnosticSeverityError {
		t.Errorf("violation = %+v, want an error on line 8", diags[0])
	}
	if !strings.Contains(diags[1].Message, "source not found") || *diags[1].Severity != protocol.DiagnosticSeverityWarning {
		t.Errorf("unrelated failure = %+v, want it kept at the failure severity", diags[1])
	}
}

func TestRenderedDiagnostics_HelmValuesRootChartNotPrefixed(t *testing.T) {
	src := yamlast.Parse([]byte(`apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: x
spec:
  chart:
    spec:
      chart: redis
  values:
    redis:
      auth: nope
    replicas: bad
`))
	diags := RenderedDiagnostics(schema.NewStore(), schema.NewResolver(), src, &render.RenderedOutput{Provider: "flate"},
		helmValuesErr("redis:\n- at '/replicas': got string, want integer\n"),
		diagnostics.Options{}, protocol.DiagnosticSeverityError)
	if len(diags) != 1 || diags[0].Range.Start.Line != 11 {
		t.Fatalf("want one diagnostic on replicas (line 11), got %+v", diags)
	}
	if want := "helm values at '/replicas': got string, want integer"; diags[0].Message != want {
		t.Errorf("message = %q, want %q", diags[0].Message, want)
	}
}

func TestUnquoteHelmPointer(t *testing.T) {
	tests := map[string]string{
		`/a/b`:          "/a/b",
		`/it\'s`:        "/it's",
		`/say "hi"`:     `/say "hi"`,
		`/tab\there`:    "/tab\there",
		`/bad\escape\q`: `/bad\escape\q`,
	}
	for in, want := range tests {
		if got := unquoteHelmPointer(in); got != want {
			t.Errorf("unquoteHelmPointer(%q) = %q, want %q", in, got, want)
		}
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
