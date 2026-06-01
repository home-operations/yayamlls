package yamlast

import (
	"strings"

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

	v := &cursorVisitor{offset: offset, best: doc.Body}
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
	best   ast.Node
}

func (v *cursorVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	tok := n.GetToken()
	if tok == nil || tok.Position == nil {
		return v
	}
	if tok.Position.Offset <= v.offset {
		v.best = n
	}
	return v
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
