// Package folding handles textDocument/foldingRange.
package folding

import (
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
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
		walkBody(doc.Body, false, &out)
	}
	return out
}

func walkBody(n ast.Node, parentIsMV bool, out *[]protocol.FoldingRange) {
	if n == nil {
		return
	}
	switch n := n.(type) {
	case *ast.MappingNode:
		if shouldEmitRangeForContainer(n, parentIsMV) {
			if r, ok := extent(n); ok {
				*out = append(*out, r)
			}
		}
		for _, v := range n.Values {
			walkBody(v, false, out)
		}
	case *ast.MappingValueNode:
		if r, ok := extent(n); ok {
			*out = append(*out, r)
		}
		walkBody(n.Key, true, out)
		walkBody(n.Value, true, out)
	case *ast.SequenceNode:
		if shouldEmitRangeForContainer(n, parentIsMV) {
			if r, ok := extent(n); ok {
				*out = append(*out, r)
			}
		}
		for _, v := range n.Values {
			walkBody(v, false, out)
		}
	}
}

// Checks whether a MappingNode or SequenceNode should emit
// its own folding range. A container should not fold when:
//   - it's nested under a MappingValueNode (parentIsMV) that already
//     covers the same lines
//   - it has only one child entry, which will produce the same range
func shouldEmitRangeForContainer(n ast.Node, parentIsMV bool) bool {
	if parentIsMV {
		return false
	}
	switch v := n.(type) {
	case *ast.MappingNode:
		return len(v.Values) != 1
	case *ast.SequenceNode:
		return len(v.Values) != 1
	}
	return false
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
		if t := endToken(c); t != nil && t.Position != nil && t.Position.Line > maxLine {
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

func endToken(n ast.Node) *token.Token {
	switch v := n.(type) {
	case *ast.MappingNode:
		return v.End
	case *ast.SequenceNode:
		return v.End
	}
	return nil
}

type visitorFn func(ast.Node)

func (v visitorFn) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	v(n)
	return v
}
