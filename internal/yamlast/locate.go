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

// EscapePointerSegment encodes a single JSON-Pointer segment (~ -> ~0,
// / -> ~1).
func EscapePointerSegment(s string) string { return escapePointerSegment(s) }

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
	r := &resolver{doc: doc}
	parent, via, ok := r.lookup(parentPtr)
	if !ok {
		return protocol.Range{}, false
	}
	n, dvia := r.deref(parent)
	if via == nil {
		via = dvia
	}
	_, kn, kvia, ok := r.field(n, key, 0)
	if !ok {
		return protocol.Range{}, false
	}
	if via == nil {
		via = kvia
	}
	if via != nil {
		return nodeRange(src, via), true
	}
	return nodeRange(src, kn), true
}

// LocateRange returns the LSP range of the node at ptr; src converts goccy's
// rune columns to UTF-16. An unresolved ptr anchors on its deepest resolvable
// ancestor. A value behind an alias or `<<` merge anchors on the alias: the
// line the user can edit or suppress.
func LocateRange(doc *ast.DocumentNode, ptr, src string) protocol.Range {
	node, via, _ := (&resolver{doc: doc}).lookup(ptr)
	if via != nil {
		node = via
	}
	if node == nil && doc != nil {
		node = doc.Body
	}
	return nodeRange(src, node)
}

// resolver follows anchors, aliases and merge keys. The anchor table is built
// on first use since most documents have no aliases.
type resolver struct {
	doc     *ast.DocumentNode
	anchors map[string]ast.Node
}

// maxAliasDepth bounds alias/merge chains so a self-referential anchor
// can't loop forever.
const maxAliasDepth = 32

type anchorCollector map[string]ast.Node

func (c anchorCollector) Visit(n ast.Node) ast.Visitor {
	if a, ok := n.(*ast.AnchorNode); ok && a.Name != nil {
		c[mapKeyString(a.Name)] = a.Value
	}
	return c
}

func (r *resolver) anchor(name string) ast.Node {
	if r.anchors == nil {
		r.anchors = map[string]ast.Node{}
		ast.Walk(anchorCollector(r.anchors), r.doc)
	}
	return r.anchors[name]
}

// deref unwraps anchor definitions and follows aliases to their anchor's
// value. via is the first alias crossed, nil when none was.
func (r *resolver) deref(n ast.Node) (out ast.Node, via ast.Node) {
	for range maxAliasDepth {
		switch x := n.(type) {
		case *ast.AnchorNode:
			n = x.Value
		case *ast.AliasNode:
			if via == nil {
				via = x
			}
			n = r.anchor(mapKeyString(x.Value))
			if n == nil {
				return nil, via
			}
		default:
			return n, via
		}
	}
	return nil, via
}

func entriesOf(n ast.Node) []*ast.MappingValueNode {
	switch n := n.(type) {
	case *ast.MappingNode:
		return n.Values
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{n}
	}
	return nil
}

func isMergeKey(n ast.Node) bool {
	_, ok := n.(*ast.MergeKeyNode)
	return ok
}

// field finds seg in the mapping n (already dereferenced). Explicit keys win
// over merged ones; among a merge sequence the earliest source wins, per the
// YAML merge spec. via is the alias that supplied a merged entry.
func (r *resolver) field(n ast.Node, seg string, depth int) (val, key, via ast.Node, ok bool) {
	if depth > maxAliasDepth {
		return nil, nil, nil, false
	}
	entries := entriesOf(n)
	for _, kv := range entries {
		if !isMergeKey(kv.Key) && mapKeyString(kv.Key) == seg {
			return kv.Value, kv.Key, nil, true
		}
	}
	for _, kv := range entries {
		if !isMergeKey(kv.Key) {
			continue
		}
		sources := []ast.Node{kv.Value}
		if seq, isSeq := kv.Value.(*ast.SequenceNode); isSeq {
			sources = seq.Values
		}
		for _, src := range sources {
			target, site := r.deref(src)
			v, k, nested, found := r.field(target, seg, depth+1)
			if !found {
				continue
			}
			if site != nil {
				nested = site
			}
			return v, k, nested, true
		}
	}
	return nil, nil, nil, false
}

// lookup resolves ptr. ok is false when a segment is missing; node (and via)
// then describe the deepest ancestor that did resolve. via is the first alias
// crossed on the way, nil when the path stays in literal nodes.
func (r *resolver) lookup(ptr string) (node, via ast.Node, ok bool) {
	if r.doc == nil || r.doc.Body == nil {
		return nil, nil, false
	}
	node = r.doc.Body
	if ptr == "" || ptr == "/" {
		return node, nil, true
	}
	for raw := range strings.SplitSeq(strings.TrimPrefix(ptr, "/"), "/") {
		next, nvia, found := r.descend(node, unescapePointerSegment(raw))
		if !found {
			return node, via, false
		}
		node = next
		if via == nil {
			via = nvia
		}
	}
	return node, via, true
}

func lookup(doc *ast.DocumentNode, ptr string) (ast.Node, bool) {
	node, _, ok := (&resolver{doc: doc}).lookup(ptr)
	return node, ok
}

// descend resolves one JSON-Pointer segment against a YAML node. The node
// type decides interpretation: a segment is matched against mapping keys by
// exact string and against sequence elements by index. This is why a
// numeric-looking mapping key (e.g. a port number) or a key containing '.'
// or '/' resolves correctly, where a YAMLPath round-trip would mis-parse it.
func (r *resolver) descend(node ast.Node, seg string) (ast.Node, ast.Node, bool) {
	n, via := r.deref(node)
	switch n := n.(type) {
	case *ast.MappingNode, *ast.MappingValueNode:
		v, _, fvia, ok := r.field(n, seg, 0)
		if via == nil {
			via = fvia
		}
		return v, via, ok
	case *ast.SequenceNode:
		if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(n.Values) {
			return n.Values[i], via, true
		}
	}
	return nil, nil, false
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

// NodeRange is the exported alias for the LSP range covering a node, computed
// from the node's full extent (handles multi-line block scalars via the
// trimmed Origin). Pass the document text as src.
func NodeRange(src string, n ast.Node) protocol.Range { return nodeRange(src, n) }

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
	l := max(line-1, 0)
	txt, _ := lineText(src, l)
	return protocol.Position{Line: uint32(l), Character: utf16Column(txt, runeCol)}
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

// LineText returns the given 0-based line of src without its trailing
// newline; ok is false when src has no such line.
func LineText(src string, line0 int) (string, bool) { return lineText(src, line0) }

func lineText(src string, line0 int) (string, bool) {
	if line0 < 0 {
		return "", false
	}
	cur, start := 0, 0
	for i := 0; i < len(src); i++ {
		if src[i] != '\n' {
			continue
		}
		if cur == line0 {
			return src[start:i], true
		}
		cur++
		start = i + 1
	}
	if cur == line0 {
		return src[start:], true
	}
	return "", false
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
