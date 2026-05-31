// Package actions handles textDocument/codeAction quick-fixes derived
// from CauseData attached to each schema diagnostic.
package actions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v5"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func Compute(uri, text string, sch *jsonschema.Schema, diags []protocol.Diagnostic) []protocol.CodeAction {
	var out []protocol.CodeAction
	for i := range diags {
		d := diags[i]
		if data, ok := decode(d.Data); ok && data.Kind == "enum" {
			out = append(out, enumActions(uri, sch, d, data)...)
		}
		if a, ok := suppressAction(uri, text, d); ok {
			out = append(out, a)
		}
	}
	return out
}

// suppressAction offers to silence a yayamlls diagnostic with a
// yayamlls-disable-line comment.
func suppressAction(uri, text string, d protocol.Diagnostic) (protocol.CodeAction, bool) {
	if d.Source == nil || *d.Source != diagnostics.Source {
		return protocol.CodeAction{}, false
	}
	line := d.Range.Start.Line
	src := lineAt(text, line)
	kind := protocol.CodeActionKindQuickFix
	var edit protocol.TextEdit
	if canTrail(src) {
		// Append the directive to the offending line so it suppresses that very
		// line. Inserting no new line keeps line numbers stable, which matters
		// for structural diagnostics (a missing or unknown key): they anchor on
		// the mapping's start line, and a directive inserted on its own line
		// above would shift down — the diagnostic re-anchors onto the directive
		// line, which the suppress-the-next-line form then misses.
		end := protocol.Position{Line: line, Character: yamlast.UTF16Len(src)}
		edit = protocol.TextEdit{
			Range:   protocol.Range{Start: end, End: end},
			NewText: " " + diagnostics.DisableLineComment(),
		}
	} else {
		// A comment-only or empty line can't take a trailing directive; put a
		// standalone one above it (suppresses the following line).
		at := protocol.Position{Line: line, Character: 0}
		edit = protocol.TextEdit{
			Range:   protocol.Range{Start: at, End: at},
			NewText: leadingWS(src) + diagnostics.DisableLineComment() + "\n",
		}
	}
	return protocol.CodeAction{
		Title:       "Suppress this diagnostic",
		Kind:        &kind,
		Diagnostics: []protocol.Diagnostic{d},
		Edit: &protocol.WorkspaceEdit{
			Changes: map[string][]protocol.TextEdit{uri: {edit}},
		},
	}, true
}

// canTrail reports whether a trailing "# yayamlls-disable-line" can be appended
// to the line as a real YAML comment: it must hold code and no existing comment
// ("#" at line start or after whitespace opens one).
func canTrail(line string) bool {
	code := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '#':
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return false
			}
			code = true
		case ' ', '\t':
		default:
			code = true
		}
	}
	return code
}

// lineAt returns the given 0-based line, or "" when out of range.
func lineAt(text string, line uint32) string {
	lines := strings.Split(text, "\n")
	if int(line) >= len(lines) {
		return ""
	}
	return lines[line]
}

// leadingWS returns the leading whitespace of a line.
func leadingWS(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func decode(raw any) (diagnostics.CauseData, bool) {
	if raw == nil {
		return diagnostics.CauseData{}, false
	}
	// Diagnostic.Data may arrive as the original Go struct OR as a
	// generic map after a JSON round-trip via the LSP transport.
	switch v := raw.(type) {
	case diagnostics.CauseData:
		return v, true
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return diagnostics.CauseData{}, false
		}
		var d diagnostics.CauseData
		if err := json.Unmarshal(b, &d); err != nil {
			return diagnostics.CauseData{}, false
		}
		return d, true
	}
	return diagnostics.CauseData{}, false
}

func enumActions(
	uri string,
	root *jsonschema.Schema,
	d protocol.Diagnostic,
	data diagnostics.CauseData,
) []protocol.CodeAction {
	if root == nil {
		return nil
	}
	target := schema.Resolve(root, data.InstanceLocation)
	if target == nil || len(target.Enum) == 0 {
		return nil
	}
	out := make([]protocol.CodeAction, 0, len(target.Enum))
	for _, v := range target.Enum {
		newText := renderEnumValue(v)
		kind := protocol.CodeActionKindQuickFix
		isPreferred := len(target.Enum) == 1
		action := protocol.CodeAction{
			Title:       fmt.Sprintf("Replace with %s", newText),
			Kind:        &kind,
			Diagnostics: []protocol.Diagnostic{d},
			IsPreferred: &isPreferred,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[string][]protocol.TextEdit{
					uri: {{Range: d.Range, NewText: newText}},
				},
			},
		}
		out = append(out, action)
	}
	return out
}

func renderEnumValue(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case bool, int, int64, float64, json.Number:
		return fmt.Sprintf("%v", v)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
