package diagnostics_test

import (
	"testing"

	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/yamlast"
)

// goccy reports the error column in code points; an astral character before
// it must widen the LSP (UTF-16) column by one.
func TestParseErrorDiagnostic_UTF16Column(t *testing.T) {
	src := "a: 1\nb: [\"😀\", x: y: z]\n"
	parsed := yamlast.Parse([]byte(src))
	if parsed.Err == nil {
		t.Fatal("expected a parse error")
	}
	got := diagnostics.ParseErrorDiagnostic(parsed.Err, src).Range.Start
	if got.Line != 1 || got.Character != 13 {
		t.Errorf("start = %v, want line 1 character 13", got)
	}
	vd := diagnostics.Validate(parsed, nil, diagnostics.Options{})
	if len(vd) != 1 || vd[0].Range.Start != got {
		t.Errorf("Validate parse-error range = %v, want start %v", vd, got)
	}
}
