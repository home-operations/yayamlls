// Package actions handles textDocument/codeAction quick-fixes derived
// from CauseData attached to each schema diagnostic.
package actions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/schema"
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

// suppressAction offers to silence a yayamlls diagnostic by inserting a
// yayamlls-disable-line comment above the offending line, matching its indent.
func suppressAction(uri, text string, d protocol.Diagnostic) (protocol.CodeAction, bool) {
	if d.Source == nil || *d.Source != diagnostics.Source {
		return protocol.CodeAction{}, false
	}
	at := protocol.Position{Line: d.Range.Start.Line, Character: 0}
	kind := protocol.CodeActionKindQuickFix
	return protocol.CodeAction{
		Title:       "Suppress this diagnostic",
		Kind:        &kind,
		Diagnostics: []protocol.Diagnostic{d},
		Edit: &protocol.WorkspaceEdit{
			Changes: map[string][]protocol.TextEdit{
				uri: {{
					Range:   protocol.Range{Start: at, End: at},
					NewText: lineIndent(text, d.Range.Start.Line) + diagnostics.DisableLineComment() + "\n",
				}},
			},
		},
	}, true
}

// lineIndent returns the leading whitespace of the given 0-based line.
func lineIndent(text string, line uint32) string {
	lines := strings.Split(text, "\n")
	if int(line) >= len(lines) {
		return ""
	}
	l := lines[line]
	return l[:len(l)-len(strings.TrimLeft(l, " \t"))]
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
