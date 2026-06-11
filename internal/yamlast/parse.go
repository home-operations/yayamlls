// Package yamlast wraps goccy/go-yaml with helpers tuned for LSP work:
// multi-doc iteration, position info, JSON-Pointer lookup, and
// AST → interface{} decoding.
package yamlast

import (
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

type Parsed struct {
	File *ast.File
	Err  error
	Text string
}

func Parse(b []byte) *Parsed {
	f, err := parser.ParseBytes(b, parser.ParseComments)
	return &Parsed{File: f, Err: err, Text: string(b)}
}

// ForCursor returns p unchanged when it parsed cleanly. On a syntax error
// the cursor line is blanked out and the document is reparsed so completion
// still has the AST above the cursor. The fallback depends on the cursor
// line, so it is recomputed per call and must never be written back to a
// cache; note its Text holds the blanked source, not the original.
func ForCursor(p *Parsed, cursorLine int) *Parsed {
	if p == nil || p.Err == nil {
		return p
	}
	lines := []byte(p.Text)
	if patched, ok := blankLine([]byte(p.Text), cursorLine); ok {
		lines = patched
	}
	return Parse(lines)
}

// ParseForCursor is the lenient variant used by completion and hover.
func ParseForCursor(text string, cursorLine int) *Parsed {
	return ForCursor(Parse([]byte(text)), cursorLine)
}

func blankLine(text []byte, target int) ([]byte, bool) {
	line := 0
	for i, b := range text {
		if line == target {
			end := i
			for end < len(text) && text[end] != '\n' {
				end++
			}
			out := make([]byte, len(text))
			copy(out, text)
			for k := i; k < end; k++ {
				out[k] = ' '
			}
			return out, true
		}
		if b == '\n' {
			line++
		}
	}
	return text, false
}

func (p *Parsed) Docs() []*ast.DocumentNode {
	if p == nil || p.File == nil {
		return nil
	}
	return p.File.Docs
}
