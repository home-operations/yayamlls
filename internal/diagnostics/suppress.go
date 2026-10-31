package diagnostics

import (
	"regexp"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Suppression directives, recognised as the first token of a YAML comment.
const (
	directiveDisableLine = "yayamlls-disable-line"
	directiveDisableFile = "yayamlls-disable-file"
	directiveDisable     = "yayamlls-disable"
	directiveEnable      = "yayamlls-enable"
)

// DisableLineComment is the YAML comment a codeAction inserts on its own line
// above an offending line; ParseSuppressions then suppresses the line below it.
func DisableLineComment() string {
	return "# " + directiveDisableLine
}

// Suppressor reports which document lines have diagnostics suppressed via
// yayamlls-disable directives in YAML comments.
type Suppressor struct {
	fileWide bool
	lines    map[uint32]bool
}

// Suppressed reports whether diagnostics on the given 0-based line are
// suppressed.
func (s Suppressor) Suppressed(line uint32) bool {
	return s.fileWide || s.lines[line]
}

// Filter drops diagnostics whose start line is suppressed, returning a new
// slice. The input is left untouched.
func (s Suppressor) Filter(diags []protocol.Diagnostic) []protocol.Diagnostic {
	out := make([]protocol.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if !s.Suppressed(d.Range.Start.Line) {
			out = append(out, d)
		}
	}
	return out
}

// ParseSuppressions scans text for yayamlls-disable directives in comments:
//
//   - "# yayamlls-disable-file" anywhere suppresses the whole file.
//   - "# yayamlls-disable" / "# yayamlls-enable" bracket a suppressed block.
//   - "# yayamlls-disable-line" suppresses its own line when trailing a value,
//     or the following line when alone on its own line.
func ParseSuppressions(text string) Suppressor {
	s := Suppressor{lines: map[uint32]bool{}}
	blocked := false
	blockIndent := -1 // parent indent of the open block scalar; -1 when none
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if blocked {
			s.lines[uint32(i)] = true
		}
		if blockIndent >= 0 && (strings.TrimSpace(line) == "" || indentOf(line) > blockIndent) {
			continue
		}
		code, comment, hasComment := line, "", false
		if ci := CommentStart(line); ci >= 0 {
			code, comment, hasComment = line[:ci], line[ci+1:], true
		}
		blockIndent = blockScalarParentIndent(code)
		if !hasComment {
			continue
		}
		switch firstToken(comment) {
		case directiveDisableFile:
			s.fileWide = true
		case directiveDisable:
			blocked = true
		case directiveEnable:
			blocked = false
		case directiveDisableLine:
			if codeBeforeComment(line) {
				s.lines[uint32(i)] = true
			} else if i+1 < len(lines) {
				s.lines[uint32(i+1)] = true
			}
		}
	}
	return s
}

// CommentStart returns the index of the "#" opening a comment on line, or -1.
// A quote opens a scalar only at a token start, so the apostrophe in `it's` is
// plain text.
func CommentStart(line string) int {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case (c == '\'' || c == '"') && atTokenStart(line, i):
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return i
		}
	}
	return -1
}

// atTokenStart reports whether i is at line start or follows `: `, `- `, `? `,
// `[`, `{` or `,`.
func atTokenStart(line string, i int) bool {
	j := i
	for j > 0 && (line[j-1] == ' ' || line[j-1] == '\t') {
		j--
	}
	if j == 0 {
		return true
	}
	switch line[j-1] {
	case '[', '{', ',':
		return true
	case ':':
		return j < i
	case '-', '?':
		return j < i && (j == 1 || line[j-2] == ' ' || line[j-2] == '\t')
	}
	return false
}

var blockScalarHeader = regexp.MustCompile(`[|>](?:[+-][1-9]?|[1-9][+-]?)?$`)

// blockScalarParentIndent returns the indent a following line must exceed to
// be inside a block scalar opened by line, or -1.
func blockScalarParentIndent(line string) int {
	code := strings.TrimRight(line, " \t")
	loc := blockScalarHeader.FindStringIndex(code)
	if loc == nil {
		return -1
	}
	before := strings.TrimRight(code[:loc[0]], " \t")
	if before != "" {
		last := before[strings.LastIndexAny(before, " \t")+1:]
		if !strings.HasSuffix(before, ":") && !strings.HasSuffix(before, "-") &&
			last[0] != '!' && last[0] != '&' {
			return -1
		}
	}
	// A `- key: |` entry's mapping sits after the dashes.
	indent := indentOf(line)
	rest := line[indent:]
	for strings.HasPrefix(rest, "- ") {
		trimmed := strings.TrimLeft(rest[1:], " ")
		indent += len(rest) - len(trimmed)
		rest = trimmed
	}
	return indent
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// codeBeforeComment reports whether non-whitespace content precedes the
// first comment marker on the line.
func codeBeforeComment(line string) bool {
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ', '\t':
			continue
		case '#':
			return false
		default:
			return true
		}
	}
	return false
}

func firstToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
