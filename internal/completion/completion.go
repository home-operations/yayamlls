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
	return enumCompletions(target)
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

func enumCompletions(s *jsonschema.Schema) *protocol.CompletionList {
	if s.Enum == nil || len(s.Enum.Values) == 0 {
		return nil
	}
	kind := protocol.CompletionItemKindEnumMember
	items := make([]protocol.CompletionItem, 0, len(s.Enum.Values))
	for _, e := range s.Enum.Values {
		items = append(items, protocol.CompletionItem{
			Label: fmt.Sprintf("%v", e),
			Kind:  &kind,
		})
	}
	return &protocol.CompletionList{Items: items}
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
