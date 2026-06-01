package completion

import (
	"fmt"
	"sort"

	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v6"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func At(text string, pos protocol.Position, sch *jsonschema.Schema) *protocol.CompletionList {
	if sch == nil {
		return nil
	}
	parsed := yamlast.ParseForCursor(text, int(pos.Line))
	ctx := yamlast.LocateCursor(parsed, text, pos)
	target := schema.Resolve(sch, ctx.Pointer)
	if target == nil {
		return nil
	}
	if ctx.IsKey {
		return propertyCompletions(target)
	}
	return valueCompletions(target)
}

func propertyCompletions(s *jsonschema.Schema) *protocol.CompletionList {
	props := schema.Properties(s)
	if len(props) == 0 {
		return nil
	}
	required := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		required[r] = true
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]protocol.CompletionItem, 0, len(keys))
	for _, k := range keys {
		ps := props[k]
		kind := propertyKind(ps)
		items = append(items, protocol.CompletionItem{
			Label:         k,
			Kind:          &kind,
			Detail:        detail(ps),
			Documentation: documentation(ps),
			InsertText:    ptrStr(k + ": "),
			SortText:      ptrStr(sortKey(k, required[k])),
		})
	}
	return &protocol.CompletionList{Items: items}
}

func valueCompletions(s *jsonschema.Schema) *protocol.CompletionList {
	values := schema.Enums(s)
	if len(values) == 0 {
		values = impliedValues(s)
	}
	if len(values) == 0 {
		return nil
	}
	kind := protocol.CompletionItemKindValue
	items := make([]protocol.CompletionItem, 0, len(values))
	for _, v := range values {
		items = append(items, protocol.CompletionItem{
			Label: fmt.Sprintf("%v", v),
			Kind:  &kind,
		})
	}
	return &protocol.CompletionList{Items: items}
}

// impliedValues offers values a schema implies without an explicit enum:
// both booleans for a boolean field, and a default if one is declared.
func impliedValues(s *jsonschema.Schema) []any {
	if s == nil {
		return nil
	}
	var out []any
	if s.Types != nil {
		for _, t := range s.Types.ToStrings() {
			if t == "boolean" {
				out = append(out, true, false)
			}
		}
	}
	if s.Default != nil {
		if d := fmt.Sprintf("%v", *s.Default); !containsStr(out, d) {
			out = append(out, *s.Default)
		}
	}
	return out
}

func containsStr(vals []any, s string) bool {
	for _, v := range vals {
		if fmt.Sprintf("%v", v) == s {
			return true
		}
	}
	return false
}

func propertyKind(s *jsonschema.Schema) protocol.CompletionItemKind {
	if s == nil {
		return protocol.CompletionItemKindField
	}
	if s.Types != nil {
		for _, t := range s.Types.ToStrings() {
			switch t {
			case "object":
				return protocol.CompletionItemKindClass
			case "array":
				return protocol.CompletionItemKindEnum
			}
		}
	}
	return protocol.CompletionItemKindField
}

func detail(s *jsonschema.Schema) *string {
	if s == nil || s.Types == nil {
		return nil
	}
	types := s.Types.ToStrings()
	if len(types) == 0 {
		return nil
	}
	t := types[0]
	return &t
}

func documentation(s *jsonschema.Schema) any {
	if s == nil || s.Description == "" {
		return nil
	}
	return protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: s.Description}
}

// sortKey biases required properties to the top of the list.
func sortKey(name string, required bool) string {
	if required {
		return "0" + name
	}
	return "1" + name
}

func ptrStr(s string) *string { return &s }
