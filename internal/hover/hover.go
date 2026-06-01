package hover

import (
	"fmt"
	"strings"

	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v6"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func At(text string, pos protocol.Position, sch *jsonschema.Schema) *protocol.Hover {
	if sch == nil {
		return nil
	}
	parsed := yamlast.ParseForCursor(text, int(pos.Line))
	ctx := yamlast.LocateCursor(parsed, text, pos)
	target := schema.Resolve(sch, ctx.Pointer)
	if target == nil {
		return nil
	}
	body := renderHoverBody(target, ctx.Pointer)
	if body == "" {
		return nil
	}
	return &protocol.Hover{
		Contents: protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: body},
	}
}

func renderHoverBody(s *jsonschema.Schema, ptr string) string {
	var b strings.Builder
	header := ptr
	if header == "" {
		header = "/"
	}
	fmt.Fprintf(&b, "**`%s`**", header)
	if s.Types != nil {
		fmt.Fprintf(&b, " — _%s_", strings.Join(s.Types.ToStrings(), " | "))
	}
	b.WriteString("\n\n")
	if s.Title != "" {
		fmt.Fprintf(&b, "**%s**\n\n", s.Title)
	}
	if s.Description != "" {
		b.WriteString(s.Description)
		b.WriteString("\n\n")
	}
	if s.Enum != nil && len(s.Enum.Values) > 0 {
		b.WriteString("**Allowed values:** ")
		parts := make([]string, 0, len(s.Enum.Values))
		for _, e := range s.Enum.Values {
			parts = append(parts, fmt.Sprintf("`%v`", e))
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString("\n")
	}
	if s.Default != nil {
		fmt.Fprintf(&b, "**Default:** `%v`\n", *s.Default)
	}
	return strings.TrimSpace(b.String())
}
