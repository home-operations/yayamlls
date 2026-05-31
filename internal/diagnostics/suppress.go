package diagnostics

import (
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
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if blocked {
			s.lines[uint32(i)] = true
		}
		comment, ok := commentOf(line)
		if !ok {
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

// commentOf returns the comment portion of a YAML line: the text after the
// "#". A "#" opens a comment only at line start or when preceded by
// whitespace, matching YAML's inline-comment rule. This is a heuristic: it
// does not track quoted strings, but only the specific directive tokens are
// acted on, so a stray "#" inside a value is harmless unless it literally
// spells a directive.
func commentOf(line string) (string, bool) {
	for i := 0; i < len(line); i++ {
		if line[i] != '#' {
			continue
		}
		if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
			return line[i+1:], true
		}
	}
	return "", false
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
