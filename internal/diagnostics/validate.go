package diagnostics

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v5"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const Source = "yayamlls"

func Validate(parsed *yamlast.Parsed, sch *jsonschema.Schema) []protocol.Diagnostic {
	if parsed == nil {
		return nil
	}
	var out []protocol.Diagnostic
	if parsed.Err != nil {
		out = append(out, parseErrorDiag(parsed.Err))
	}
	if sch == nil {
		return out
	}
	for _, doc := range parsed.Docs() {
		out = append(out, validateDoc(doc, sch, parsed.Text)...)
	}
	return out
}

// ValidateDoc runs schema validation against a single YAML document. src is
// the document text, used to place diagnostics at UTF-16 columns. Returns
// nil when sch is nil or the doc validates cleanly.
func ValidateDoc(doc *ast.DocumentNode, sch *jsonschema.Schema, src string) []protocol.Diagnostic {
	if sch == nil {
		return nil
	}
	return validateDoc(doc, sch, src)
}

// ParseErrorDiagnostic produces the file-level diagnostic for a YAML
// parse failure. Exposed so the LSP layer can surface it once per file
// rather than once per doc.
func ParseErrorDiagnostic(err error) protocol.Diagnostic {
	return parseErrorDiag(err)
}

func validateDoc(doc *ast.DocumentNode, sch *jsonschema.Schema, src string) []protocol.Diagnostic {
	value, err := yamlast.Decode(doc)
	if err != nil {
		return []protocol.Diagnostic{{
			Severity: ptr(protocol.DiagnosticSeverityError),
			Source:   ptr(Source),
			Message:  fmt.Sprintf("decode: %v", err),
			Range:    yamlast.LocateRange(doc, "", src),
		}}
	}
	if err := sch.Validate(value); err != nil {
		var verr *jsonschema.ValidationError
		if errors.As(err, &verr) {
			return flattenValidationError(doc, verr, src)
		}
		return []protocol.Diagnostic{{
			Severity: ptr(protocol.DiagnosticSeverityError),
			Source:   ptr(Source),
			Message:  err.Error(),
			Range:    yamlast.LocateRange(doc, "", src),
		}}
	}
	return nil
}

// CauseData rides on Diagnostic.Data so codeAction handlers can produce
// quick-fixes without re-validating.
type CauseData struct {
	Kind             string `json:"kind"`             // jsonschema keyword: enum, required, type, etc.
	InstanceLocation string `json:"instanceLocation"` // JSON Pointer into the source document
}

// flattenValidationError emits one diagnostic per leaf cause. Leaves
// carry the actionable message; root nodes are generic wrappers.
func flattenValidationError(doc *ast.DocumentNode, verr *jsonschema.ValidationError, src string) []protocol.Diagnostic {
	var out []protocol.Diagnostic
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			out = append(out, protocol.Diagnostic{
				Severity: ptr(protocol.DiagnosticSeverityError),
				Source:   ptr(Source),
				Message:  fmt.Sprintf("%s (at %s)", e.Message, displayPointer(e.InstanceLocation)),
				Range:    leafRange(doc, e, src),
				Data:     dataFor(e),
			})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(verr)
	return out
}

// leafRange anchors a diagnostic on the most specific line. Most errors point
// at their instance value, but additionalProperties and required report against
// a parent object — which would anchor on the object's first child. Steer them
// to the offending key (additionalProperties) or the owning key (required).
func leafRange(doc *ast.DocumentNode, e *jsonschema.ValidationError, src string) protocol.Range {
	switch keywordOf(e.KeywordLocation) {
	case "additionalProperties":
		if name, ok := additionalPropName(e.Message); ok {
			if r, ok := yamlast.LocateKey(doc, e.InstanceLocation, name, src); ok {
				return r
			}
		}
	case "required":
		if parent, key := splitPointer(e.InstanceLocation); key != "" {
			if r, ok := yamlast.LocateKey(doc, parent, yamlast.UnescapePointerSegment(key), src); ok {
				return r
			}
		}
	}
	return yamlast.LocateRange(doc, e.InstanceLocation, src)
}

// additionalPropName extracts the first offending property from an
// "additionalProperties 'x', 'y' not allowed" message.
func additionalPropName(msg string) (string, bool) {
	rest, found := strings.CutPrefix(msg, "additionalProperties ")
	if !found {
		return "", false
	}
	a := strings.IndexByte(rest, '\'')
	if a < 0 {
		return "", false
	}
	rest = rest[a+1:]
	b := strings.IndexByte(rest, '\'')
	if b < 0 {
		return "", false
	}
	return rest[:b], true
}

// splitPointer splits a JSON pointer into its parent pointer and last segment.
func splitPointer(ptr string) (parent, key string) {
	i := strings.LastIndexByte(ptr, '/')
	if i < 0 {
		return "", ptr
	}
	return ptr[:i], ptr[i+1:]
}

func dataFor(e *jsonschema.ValidationError) any {
	kind := keywordOf(e.KeywordLocation)
	if kind == "" {
		return nil
	}
	return CauseData{Kind: kind, InstanceLocation: e.InstanceLocation}
}

// keywordOf returns the last segment of a JSON-Pointer keyword location.
func keywordOf(p string) string {
	if p == "" {
		return ""
	}
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

func displayPointer(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

func parseErrorDiag(err error) protocol.Diagnostic {
	rng, msg := parseErrorLocation(err.Error())
	return protocol.Diagnostic{
		Severity: ptr(protocol.DiagnosticSeverityError),
		Source:   ptr(Source),
		Message:  msg,
		Range:    rng,
	}
}

// parseErrorLocation pulls the position out of goccy's syntax-error text,
// which is formatted as "[line:col] message" followed by a source snippet.
// The snippet is dropped and the position anchors the diagnostic; if the
// prefix is absent the message is returned as-is at the document start.
func parseErrorLocation(s string) (protocol.Range, string) {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if !strings.HasPrefix(s, "[") {
		return protocol.Range{}, s
	}
	close := strings.IndexByte(s, ']')
	if close < 0 {
		return protocol.Range{}, s
	}
	line, col, ok := splitLineCol(s[1:close])
	if !ok {
		return protocol.Range{}, s
	}
	pos := protocol.Position{Line: oneBasedToZero(line), Character: oneBasedToZero(col)}
	rng := protocol.Range{Start: pos, End: protocol.Position{Line: pos.Line, Character: pos.Character + 1}}
	return rng, strings.TrimSpace(s[close+1:])
}

func splitLineCol(s string) (line, col int, ok bool) {
	colon := strings.IndexByte(s, ':')
	if colon < 0 {
		return 0, 0, false
	}
	line, err1 := strconv.Atoi(s[:colon])
	col, err2 := strconv.Atoi(s[colon+1:])
	return line, col, err1 == nil && err2 == nil
}

func oneBasedToZero(n int) uint32 {
	if n > 0 {
		return uint32(n - 1)
	}
	return 0
}

func ptr[T any](v T) *T { return &v }
