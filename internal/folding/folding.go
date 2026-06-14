// Package folding handles textDocument/foldingRange.
package folding

import (
	"github.com/goccy/go-yaml/ast"
	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func Ranges(parsed *yamlast.Parsed) []protocol.FoldingRange {
	if parsed == nil || parsed.File == nil {
		return nil
	}
	var out []protocol.FoldingRange
	for _, doc := range parsed.File.Docs {
		if doc == nil || doc.Body == nil {
			continue
		}
		ast.Walk(visitorFn(func(n ast.Node) {
			switch n.(type) {
			case *ast.DocumentNode, *ast.MappingNode, *ast.SequenceNode:
				if r, ok := extent(n); ok {
					out = append(out, r)
				}
			}
		}), doc)
	}
	return out
}

func extent(n ast.Node) (protocol.FoldingRange, bool) {
	startTok := n.GetToken()
	if startTok == nil || startTok.Position == nil {
		return protocol.FoldingRange{}, false
	}
	maxLine := startTok.Position.Line
	ast.Walk(visitorFn(func(c ast.Node) {
		if t := c.GetToken(); t != nil && t.Position != nil && t.Position.Line > maxLine {
			maxLine = t.Position.Line
		}
	}), n)
	startLine := uint32(startTok.Position.Line - 1)
	endLine := uint32(maxLine - 1)
	if endLine <= startLine {
		return protocol.FoldingRange{}, false
	}
	kind := string(protocol.FoldingRangeKindRegion)
	return protocol.FoldingRange{StartLine: startLine, EndLine: endLine, Kind: &kind}, true
}

type visitorFn func(ast.Node)

func (v visitorFn) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	v(n)
	return v
}
