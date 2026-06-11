package completion

import (
	"fmt"
	"sort"
	"strings"

	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v6"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Options carries client capabilities that shape completion items.
type Options struct {
	// Snippets is whether the client advertised
	// textDocument.completion.completionItem.snippetSupport.
	Snippets bool
}

func At(parsed *yamlast.Parsed, pos protocol.Position, sch *jsonschema.Schema, opts Options) *protocol.CompletionList {
	if sch == nil || parsed == nil {
		return nil
	}
	p := yamlast.ForCursor(parsed, int(pos.Line))
	ctx := yamlast.LocateCursor(p, parsed.Text, pos)
	target := schema.Resolve(sch, ctx.Pointer)
	if target == nil {
		return nil
	}
	ic := insertContext(parsed.Text, pos, opts)
	if ctx.IsKey {
		return propertyCompletions(target, ic)
	}
	return valueCompletions(target, ic)
}

// insertCtx captures how completion text must be shaped at the cursor:
// snippet capability, the indent for snippet continuation lines, and whether
// a separating space is needed because the cursor sits right after a bare
// ":" or "-" (the trigger characters fire before the user types one).
type insertCtx struct {
	snippets  bool
	indent    string
	needSpace bool
}

func insertContext(text string, pos protocol.Position, opts Options) insertCtx {
	prefix := yamlast.LinePrefix(text, pos)
	return insertCtx{
		snippets:  opts.Snippets,
		indent:    relIndent(prefix),
		needSpace: strings.HasSuffix(prefix, ":") || strings.HasSuffix(prefix, "-"),
	}
}

// relIndent computes the indent for snippet continuation lines. Snippet
// clients prepend the insertion line's leading whitespace to every
// continuation line, so the result is relative to it: children of the
// completed key sit two spaces past where the typed word starts.
func relIndent(prefix string) string {
	ws := 0
	for ws < len(prefix) && (prefix[ws] == ' ' || prefix[ws] == '\t') {
		ws++
	}
	wordStart := strings.LastIndexByte(prefix, ' ') + 1
	if strings.HasSuffix(prefix, "-") {
		// The dash trigger inserts " key", placing the word one past the dash.
		wordStart = len(prefix) + 1
	}
	if wordStart < ws {
		wordStart = ws
	}
	return strings.Repeat(" ", wordStart-ws+2)
}

func propertyCompletions(s *jsonschema.Schema, ic insertCtx) *protocol.CompletionList {
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
		insert, isSnippet := insertTextForProperty(k, ps, ic)
		if ic.needSpace {
			insert = " " + insert
		}
		item := protocol.CompletionItem{
			Label:         k,
			Kind:          &kind,
			Detail:        detail(ps),
			Documentation: documentation(ps),
			InsertText:    ptrStr(insert),
			SortText:      ptrStr(sortKey(k, required[k])),
		}
		if isSnippet {
			f := protocol.InsertTextFormatSnippet
			item.InsertTextFormat = &f
		}
		items = append(items, item)
	}
	return &protocol.CompletionList{Items: items}
}

// insertTextForProperty shapes what accepting property k inserts. Without
// snippet support it stays the plain "k: "; with it, a tab stop lands where
// the value goes: objects open an indented block (one level of required
// children pre-expanded, no recursion), arrays open an item, scalars carry
// their default as the placeholder.
func insertTextForProperty(k string, ps *jsonschema.Schema, ic insertCtx) (string, bool) {
	if !ic.snippets {
		return k + ": ", false
	}
	rs := schema.Resolve(ps, "")
	switch {
	case isType(rs, "object"):
		if req := schema.Required(rs); len(req) > 0 {
			sort.Strings(req)
			var b strings.Builder
			b.WriteString(k + ":")
			for i, child := range req {
				fmt.Fprintf(&b, "\n%s%s: $%d", ic.indent, child, i+1)
			}
			return b.String(), true
		}
		return k + ":\n" + ic.indent + "$1", true
	case isType(rs, "array"):
		return k + ":\n" + ic.indent + "- $1", true
	default:
		if rs != nil && rs.Default != nil {
			return fmt.Sprintf("%s: ${1:%s}", k, snippetEscape(fmt.Sprintf("%v", *rs.Default))), true
		}
		return k + ": $1", true
	}
}

func isType(s *jsonschema.Schema, t string) bool {
	if s == nil || s.Types == nil {
		return false
	}
	for _, st := range s.Types.ToStrings() {
		if st == t {
			return true
		}
	}
	return false
}

// snippetEscape escapes the characters the LSP snippet grammar reserves.
var snippetEscaper = strings.NewReplacer(`\`, `\\`, `$`, `\$`, `}`, `\}`)

func snippetEscape(s string) string { return snippetEscaper.Replace(s) }

func valueCompletions(s *jsonschema.Schema, ic insertCtx) *protocol.CompletionList {
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
		item := protocol.CompletionItem{
			Label: fmt.Sprintf("%v", v),
			Kind:  &kind,
		}
		if ic.needSpace {
			// Right after "key:": insert " value" but keep the label bare so
			// client-side filtering still matches what the user types.
			item.InsertText = ptrStr(" " + item.Label)
		}
		items = append(items, item)
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
