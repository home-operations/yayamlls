// Package symbols handles textDocument/documentSymbol.
package symbols

import (
	"fmt"

	yaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func Outline(parsed *yamlast.Parsed) []protocol.DocumentSymbol {
	if parsed == nil || parsed.File == nil {
		return nil
	}
	text := parsed.Text
	var out []protocol.DocumentSymbol
	for _, doc := range parsed.File.Docs {
		if doc.Body == nil {
			continue
		}
		out = append(out, documentSymbol(doc, text))
	}
	return out
}

func documentSymbol(doc *ast.DocumentNode, src string) protocol.DocumentSymbol {
	name := docLabel(doc)
	r := yamlast.LocateRange(doc, "", src)
	sym := protocol.DocumentSymbol{
		Name:           name,
		Kind:           protocol.SymbolKindNamespace,
		Range:          r,
		SelectionRange: r,
	}
	if m, ok := doc.Body.(*ast.MappingNode); ok {
		sym.Children = mappingChildren(m, src)
	}
	return sym
}

func docLabel(doc *ast.DocumentNode) string {
	if gvk, ok := schema.DetectGVK(doc.Body); ok {
		var meta struct {
			Metadata struct {
				Name      string `yaml:"name"`
				Namespace string `yaml:"namespace"`
			} `yaml:"metadata"`
		}
		_ = yaml.NodeToValue(doc.Body, &meta)
		if meta.Metadata.Name != "" {
			if meta.Metadata.Namespace != "" {
				return fmt.Sprintf("%s/%s.%s", gvk.Kind, meta.Metadata.Name, meta.Metadata.Namespace)
			}
			return fmt.Sprintf("%s/%s", gvk.Kind, meta.Metadata.Name)
		}
		return gvk.Kind
	}
	return "document"
}

func mappingChildren(m *ast.MappingNode, src string) []protocol.DocumentSymbol {
	if m == nil {
		return nil
	}
	out := make([]protocol.DocumentSymbol, 0, len(m.Values))
	for _, kv := range m.Values {
		out = append(out, mappingEntrySymbol(kv, src))
	}
	return out
}

func mappingEntrySymbol(kv *ast.MappingValueNode, src string) protocol.DocumentSymbol {
	keyName := keyString(kv.Key)
	kind, detail := classify(kv.Value)
	r := nodeRange(kv.Key, src)
	sym := protocol.DocumentSymbol{
		Name:           keyName,
		Kind:           kind,
		Range:          r,
		SelectionRange: r,
	}
	if detail != "" {
		sym.Detail = &detail
	}
	switch v := kv.Value.(type) {
	case *ast.MappingNode:
		sym.Children = mappingChildren(v, src)
	case *ast.SequenceNode:
		sym.Children = sequenceChildren(v, src)
	}
	return sym
}

func sequenceChildren(s *ast.SequenceNode, src string) []protocol.DocumentSymbol {
	if s == nil {
		return nil
	}
	out := make([]protocol.DocumentSymbol, 0, len(s.Values))
	for i, item := range s.Values {
		kind, detail := classify(item)
		r := nodeRange(item, src)
		sym := protocol.DocumentSymbol{
			Name:           fmt.Sprintf("[%d]", i),
			Kind:           kind,
			Range:          r,
			SelectionRange: r,
		}
		if detail != "" {
			sym.Detail = &detail
		}
		switch v := item.(type) {
		case *ast.MappingNode:
			sym.Children = mappingChildren(v, src)
		case *ast.SequenceNode:
			sym.Children = sequenceChildren(v, src)
		}
		out = append(out, sym)
	}
	return out
}

func classify(n ast.Node) (protocol.SymbolKind, string) {
	if n == nil {
		return protocol.SymbolKindNull, ""
	}
	switch v := n.(type) {
	case *ast.MappingNode:
		return protocol.SymbolKindObject, ""
	case *ast.SequenceNode:
		return protocol.SymbolKindArray, ""
	case *ast.StringNode:
		return protocol.SymbolKindString, v.Value
	case *ast.IntegerNode:
		return protocol.SymbolKindNumber, fmt.Sprintf("%v", v.Value)
	case *ast.FloatNode:
		return protocol.SymbolKindNumber, fmt.Sprintf("%v", v.Value)
	case *ast.BoolNode:
		return protocol.SymbolKindBoolean, fmt.Sprintf("%v", v.Value)
	case *ast.NullNode:
		return protocol.SymbolKindNull, "null"
	}
	return protocol.SymbolKindField, ""
}

func keyString(n ast.Node) string {
	if s, ok := n.(*ast.StringNode); ok {
		return s.Value
	}
	if n != nil {
		return n.String()
	}
	return "?"
}

func nodeRange(n ast.Node, src string) protocol.Range {
	if n == nil {
		return protocol.Range{}
	}
	tok := n.GetToken()
	if tok == nil || tok.Position == nil {
		return protocol.Range{}
	}
	// goccy reports rune-based columns; LSP wants UTF-16 code units.
	start := yamlast.UTF16Position(src, tok.Position.Line, tok.Position.Column)
	return protocol.Range{
		Start: start,
		End:   protocol.Position{Line: start.Line, Character: start.Character + yamlast.UTF16Len(tok.Value)},
	}
}
