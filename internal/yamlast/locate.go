package yamlast

import (
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func unescapePointerSegment(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
}

// UnescapePointerSegment decodes a single JSON-Pointer segment (~1 -> /,
// ~0 -> ~).
func UnescapePointerSegment(s string) string { return unescapePointerSegment(s) }

func StringValueAt(doc *ast.DocumentNode, ptr string) (string, bool) {
	node, ok := lookup(doc, ptr)
	if !ok {
		return "", false
	}
	s, ok := node.(*ast.StringNode)
	if !ok {
		return "", false
	}
	return s.Value, true
}

// TagAt returns the YAML tag (e.g. "!Ref") on the node at ptr, if any. A
// custom-tagged value such as `replicas: !Ref count` decodes to its bare
// scalar for schema validation; callers use the tag to recognise externally
// resolved values and skip validating them.
func TagAt(doc *ast.DocumentNode, ptr string) (string, bool) {
	node, ok := lookup(doc, ptr)
	if !ok {
		return "", false
	}
	tn, ok := node.(*ast.TagNode)
	if !ok || tn.Start == nil {
		return "", false
	}
	return tn.Start.Value, true
}

// LocateKey returns the range of the `key` token within the mapping at
// parentPtr, for anchoring structural diagnostics (an unknown or missing
// property) on a specific key line rather than the whole mapping. key is a
// raw key, already unescaped. ok is false when the mapping or key isn't found.
func LocateKey(doc *ast.DocumentNode, parentPtr, key, src string) (protocol.Range, bool) {
	parent, ok := lookup(doc, parentPtr)
	if !ok {
		return protocol.Range{}, false
	}
	kn := keyNode(parent, key)
	if kn == nil {
		return protocol.Range{}, false
	}
	return nodeRange(src, kn), true
}

func keyNode(parent ast.Node, key string) ast.Node {
	switch n := parent.(type) {
	case *ast.MappingNode:
		for _, kv := range n.Values {
			if mapKeyString(kv.Key) == key {
				return kv.Key
			}
		}
	case *ast.MappingValueNode:
		if mapKeyString(n.Key) == key {
			return n.Key
		}
	}
	return nil
}

// LocateRange returns the LSP range that covers the node at ptr. src is the
// document text, needed to translate goccy's rune-based columns into the
// UTF-16 code units LSP uses. Falls back to the document body's range when
// ptr can't be resolved, common for `required` violations where the field
// is simply missing.
func LocateRange(doc *ast.DocumentNode, ptr, src string) protocol.Range {
	node, ok := lookup(doc, ptr)
	if !ok {
		node = doc.Body
	}
	return nodeRange(src, node)
}

func lookup(doc *ast.DocumentNode, ptr string) (ast.Node, bool) {
	if doc == nil || doc.Body == nil {
		return nil, false
	}
	if ptr == "" || ptr == "/" {
		return doc.Body, true
	}
	node := doc.Body
	for _, raw := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		next, ok := descend(node, unescapePointerSegment(raw))
		if !ok {
			return nil, false
		}
		node = next
	}
	return node, true
}

// descend resolves one JSON-Pointer segment against a YAML node. The node
// type decides interpretation: a segment is matched against mapping keys by
// exact string and against sequence elements by index. This is why a
// numeric-looking mapping key (e.g. a port number) or a key containing '.'
// or '/' resolves correctly, where a YAMLPath round-trip would mis-parse it.
func descend(node ast.Node, seg string) (ast.Node, bool) {
	switch n := node.(type) {
	case *ast.MappingNode:
		for _, kv := range n.Values {
			if mapKeyString(kv.Key) == seg {
				return kv.Value, true
			}
		}
	case *ast.MappingValueNode:
		if mapKeyString(n.Key) == seg {
			return n.Value, true
		}
	case *ast.SequenceNode:
		if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(n.Values) {
			return n.Values[i], true
		}
	}
	return nil, false
}

func mapKeyString(n ast.Node) string {
	if s, ok := n.(*ast.StringNode); ok {
		return s.Value
	}
	if n != nil {
		if tok := n.GetToken(); tok != nil {
			return tok.Value
		}
	}
	return ""
}

func nodeRange(src string, n ast.Node) protocol.Range {
	if n == nil {
		return protocol.Range{}
	}
	v := &extentVisitor{}
	ast.Walk(v, n)
	if v.start == nil {
		if tok := n.GetToken(); tok != nil && tok.Position != nil {
			v.start, v.end = tok, tok
		} else {
			return protocol.Range{}
		}
	}

	start := tokenStart(src, v.start)
	end := tokenEnd(src, v.end)
	r := protocol.Range{Start: start, End: end}
	if r.End.Line < r.Start.Line || (r.End.Line == r.Start.Line && r.End.Character <= r.Start.Character) {
		r.End = protocol.Position{Line: start.Line, Character: start.Character + 1}
	}
	return r
}

func tokenStart(src string, t *token.Token) protocol.Position {
	if t == nil || t.Position == nil {
		return protocol.Position{}
	}
	return UTF16Position(src, t.Position.Line, t.Position.Column)
}

// tokenEnd returns the position just past the token's meaningful content.
// goccy's Origin includes the whitespace preceding the token (e.g. the
// space after `:`) but Position is anchored to the first meaningful rune,
// so for single-line tokens we measure Value; for multi-line content we
// walk the trimmed Origin to count newlines. Widths are in UTF-16 units.
func tokenEnd(src string, t *token.Token) protocol.Position {
	start := tokenStart(src, t)
	line, col := start.Line, start.Character
	body := t.Value
	if body == "" || !strings.ContainsRune(body, '\n') && !strings.ContainsRune(t.Origin, '\n') {
		return protocol.Position{Line: line, Character: col + UTF16Len(body)}
	}
	body = strings.TrimLeft(t.Origin, " \t")
	body = strings.TrimRight(body, " \t\n")
	for _, r := range body {
		if r == '\n' {
			line++
			col = 0
			continue
		}
		col += utf16RuneLen(r)
	}
	return protocol.Position{Line: line, Character: col}
}

// UTF16Position converts goccy's 1-based (line, runeCol), whose column
// counts Unicode code points, into a 0-based LSP position whose character
// counts UTF-16 code units. src supplies the line so columns past a
// non-BMP rune are translated correctly.
func UTF16Position(src string, line, runeCol int) protocol.Position {
	l := line - 1
	if l < 0 {
		l = 0
	}
	return protocol.Position{Line: uint32(l), Character: utf16Column(lineText(src, l), runeCol)}
}

func utf16Column(text string, runeCol int) uint32 {
	if runeCol <= 1 {
		return 0
	}
	target := runeCol - 1
	var col uint32
	idx := 0
	for _, r := range text {
		if idx >= target {
			return col
		}
		col += utf16RuneLen(r)
		idx++
	}
	if idx < target {
		// Source shorter than the reported column (e.g. unavailable): fall
		// back to one unit per remaining rune so the result stays monotonic.
		col += uint32(target - idx)
	}
	return col
}

// UTF16Len returns the number of UTF-16 code units in s.
func UTF16Len(s string) uint32 {
	var n uint32
	for _, r := range s {
		n += utf16RuneLen(r)
	}
	return n
}

func utf16RuneLen(r rune) uint32 {
	if n := utf16.RuneLen(r); n > 0 {
		return uint32(n)
	}
	return 1
}

func lineText(src string, line0 int) string {
	if src == "" || line0 < 0 {
		return ""
	}
	cur, start := 0, 0
	for i := 0; i < len(src); i++ {
		if src[i] != '\n' {
			continue
		}
		if cur == line0 {
			return src[start:i]
		}
		cur++
		start = i + 1
	}
	if cur == line0 {
		return src[start:]
	}
	return ""
}

type extentVisitor struct{ start, end *token.Token }

func (v *extentVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	if t := n.GetToken(); t != nil && t.Position != nil {
		if v.start == nil || t.Position.Offset < v.start.Position.Offset {
			v.start = t
		}
		if v.end == nil || t.Position.Offset > v.end.Position.Offset {
			v.end = t
		}
	}
	return v
}
