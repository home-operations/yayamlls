package yamlast

import (
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type CursorContext struct {
	Pointer string
	IsKey   bool
	Doc     *ast.DocumentNode
}

// LocateCursor returns the enclosing structural context at pos. The AST
// is consulted first; when it carries no information for the cursor we
// fall back to an indent-based text walker so completion still works
// while the user is typing.
func LocateCursor(parsed *Parsed, text string, pos protocol.Position) CursorContext {
	if parsed == nil || parsed.File == nil || len(parsed.File.Docs) == 0 {
		return CursorContext{
			Pointer: inferPathByIndent(text, int(pos.Line)),
			IsKey:   isKeyLine(text, pos),
		}
	}
	offset := offsetOf(text, pos)
	doc := parsed.File.Docs[0]
	for _, d := range parsed.File.Docs {
		if d.Body == nil {
			continue
		}
		if tok := d.Body.GetToken(); tok != nil && tok.Position != nil && tok.Position.Offset <= offset {
			doc = d
		}
	}
	if doc == nil || doc.Body == nil {
		// No AST to walk (empty or whitespace-only document): fall back to
		// the text heuristics, same as when there are no docs at all.
		return CursorContext{
			Pointer: inferPathByIndent(text, int(pos.Line)),
			IsKey:   isKeyLine(text, pos),
			Doc:     doc,
		}
	}
	ctx := CursorContext{Doc: doc}

	v := &cursorVisitor{
		offset: offset,
		line:   int(pos.Line) + 1, // goccy positions are 1-based
		col:    cursorColumn(text, pos),
		best:   doc.Body,
	}
	ast.Walk(v, doc.Body)

	ctx.Pointer = pathToPointer(v.best.GetPath())
	if ctx.Pointer == "" {
		ctx.Pointer = inferPathByIndent(text, int(pos.Line))
	}
	if isKeyLine(text, pos) {
		ctx.IsKey = true
	}
	return ctx
}

type cursorVisitor struct {
	offset int
	line   int // 1-based cursor line, matching goccy token positions
	col    int // 1-based cursor column (rune count), matching goccy
	best   ast.Node
}

func (v *cursorVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	if v.encloses(n) {
		v.best = n
	}
	return v
}

// encloses reports whether the cursor sits structurally within n. YAML
// structure is indentation-defined, so a purely offset-based test is wrong:
// on a fresh continuation line the cursor's offset lands past the previous
// sibling's leaf value, which would attach the cursor to that sibling rather
// than to the enclosing mapping. We therefore gate on the cursor's column
// (indentation) as well as its offset.
func (v *cursorVisitor) encloses(n ast.Node) bool {
	tok := n.GetToken()
	if tok == nil || tok.Position == nil {
		return false
	}
	if tok.Position.Offset > v.offset {
		return false // n begins after the cursor
	}
	switch t := n.(type) {
	case *ast.MappingNode:
		// Inside the mapping when the cursor is at or deeper than its keys:
		// a cursor at the key column is a (possibly new) sibling key.
		return v.col >= mappingEntryColumn(t)
	case *ast.SequenceNode:
		// Inside the sequence when the cursor is at or deeper than its dashes.
		return v.col >= tok.Position.Column
	case *ast.MappingValueNode:
		// A mapping entry is the cursor's target only at an inline value
		// position: on the value's own line, past the key. A cursor on a
		// later line is either a sibling key (handled by the enclosing
		// mapping) or inside a block value (handled when its container node
		// is walked), never this scalar entry itself.
		return tok.Position.Line == v.line && v.col > keyColumn(t)
	default:
		// Scalar (including a null placeholder for an empty value): the cursor
		// belongs to it only while editing on its own line.
		return tok.Position.Line == v.line
	}
}

// keyColumn reports the indentation column of a mapping entry's key. goccy
// anchors the MappingValueNode token at the ':' rather than the key, so the
// column is read from the key node, falling back to the ':' for malformed
// entries.
func keyColumn(mv *ast.MappingValueNode) int {
	if c := tokenColumn(mv.Key); c > 0 {
		return c
	}
	if c := tokenColumn(mv); c > 0 {
		return c
	}
	return 1
}

// mappingEntryColumn reports the column a mapping's keys sit at, taken from its
// first entry since goccy anchors the MappingNode token at the first ':'.
func mappingEntryColumn(m *ast.MappingNode) int {
	if len(m.Values) > 0 {
		return keyColumn(m.Values[0])
	}
	if c := tokenColumn(m); c > 0 {
		return c
	}
	return 1
}

func tokenColumn(n ast.Node) int {
	if n == nil {
		return 0
	}
	if t := n.GetToken(); t != nil && t.Position != nil {
		return t.Position.Column
	}
	return 0
}

// cursorColumn returns the cursor's 1-based rune column, matching goccy's
// token columns. Indentation is ASCII whitespace, so a rune count is the
// right unit for comparing against key/dash columns.
func cursorColumn(text string, pos protocol.Position) int {
	lines := strings.Split(text, "\n")
	if int(pos.Line) >= len(lines) {
		return 1
	}
	return utf8.RuneCountInString(lineUpToUTF16(lines[pos.Line], pos.Character)) + 1
}

// pathToPointer converts goccy's YAMLPath (e.g. `$.a.'b.c'[0]`) into an RFC
// 6901 JSON Pointer. goccy single-quotes keys containing metacharacters, so
// a quoted segment is taken verbatim (dots inside it are part of the key),
// and every key segment is JSON-Pointer escaped so '/' and '~' survive. The
// indent fallback uses the same escaping, keeping both paths consistent.
func pathToPointer(yp string) string {
	rest := strings.TrimPrefix(yp, "$")
	if rest == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(rest); {
		switch rest[i] {
		case '.':
			i++
			if i < len(rest) && rest[i] == '\'' {
				i++
				start := i
				for i < len(rest) && rest[i] != '\'' {
					i++
				}
				seg := rest[start:i]
				if i < len(rest) {
					i++ // skip closing quote
				}
				b.WriteByte('/')
				b.WriteString(escapePointerSegment(seg))
			} else {
				start := i
				for i < len(rest) && rest[i] != '.' && rest[i] != '[' {
					i++
				}
				b.WriteByte('/')
				b.WriteString(escapePointerSegment(rest[start:i]))
			}
		case '[':
			i++
			start := i
			for i < len(rest) && rest[i] != ']' {
				i++
			}
			b.WriteByte('/')
			b.WriteString(rest[start:i])
			if i < len(rest) {
				i++ // skip closing ']'
			}
		default:
			i++
		}
	}
	return b.String()
}

func offsetOf(text string, pos protocol.Position) int {
	line, col := uint32(0), uint32(0)
	for i, r := range text {
		if line == pos.Line {
			if col >= pos.Character {
				return i
			}
			if r == '\n' {
				return i // column past end of line: clamp to line end
			}
		}
		if r == '\n' {
			line++
			col = 0
			continue
		}
		// pos.Character is a UTF-16 code-unit count; advance by the rune's
		// UTF-16 width so cursors after non-BMP runes map to the right node.
		col += utf16RuneLen(r)
	}
	return len(text)
}

func isKeyLine(text string, pos protocol.Position) bool {
	lines := strings.Split(text, "\n")
	if int(pos.Line) >= len(lines) {
		return false
	}
	return !strings.Contains(lineUpToUTF16(lines[pos.Line], pos.Character), ":")
}

// lineUpToUTF16 returns the prefix of line preceding the given UTF-16
// column, or all of line when the column is at or past its end.
func lineUpToUTF16(line string, utf16Col uint32) string {
	var col uint32
	for i, r := range line {
		if col >= utf16Col {
			return line[:i]
		}
		col += utf16RuneLen(r)
	}
	return line
}
