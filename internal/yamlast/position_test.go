package yamlast

import (
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// "😀" occupies two UTF-16 code units but four bytes; OffsetAt must count
// the former and return the latter, or edits and cursor mapping in
// documents with astral-plane runes land at the wrong byte.
func TestOffsetAt_AstralColumnsAreUTF16(t *testing.T) {
	text := "a: 😀x\n"
	cases := []struct {
		name string
		pos  protocol.Position
		want int
	}{
		{"start of line", protocol.Position{Line: 0, Character: 0}, 0},
		{"before emoji", protocol.Position{Line: 0, Character: 3}, 3},
		{"after emoji (two code units)", protocol.Position{Line: 0, Character: 5}, 7},
		{"after trailing rune", protocol.Position{Line: 0, Character: 6}, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OffsetAt(text, tc.pos); got != tc.want {
				t.Errorf("OffsetAt(%v) = %d, want %d", tc.pos, got, tc.want)
			}
		})
	}
}

func TestOffsetAt_ClampsPastLineEnd(t *testing.T) {
	text := "ab\ncd\n"
	// Character 99 on line 0 clamps to the newline, not into line 1.
	if got := OffsetAt(text, protocol.Position{Line: 0, Character: 99}); got != 2 {
		t.Errorf("past line end = %d, want 2", got)
	}
}

func TestOffsetAt_ClampsPastDocumentEnd(t *testing.T) {
	text := "ab\ncd"
	if got := OffsetAt(text, protocol.Position{Line: 9, Character: 0}); got != len(text) {
		t.Errorf("past document end = %d, want %d", got, len(text))
	}
}

func TestOffsetAt_SecondLine(t *testing.T) {
	text := "ab\ncd\n"
	if got := OffsetAt(text, protocol.Position{Line: 1, Character: 1}); got != 4 {
		t.Errorf("line 1 char 1 = %d, want 4", got)
	}
}

func TestLineText(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		line   int
		want   string
		wantOK bool
	}{
		{"first line", "a\nb\nc", 0, "a", true},
		{"middle line", "a\nb\nc", 1, "b", true},
		{"last line without trailing newline", "a\nb\nc", 2, "c", true},
		{"empty line after trailing newline", "a\nb\n", 2, "", true},
		{"out of range", "a\nb", 2, "", false},
		{"negative", "a", -1, "", false},
		{"empty text has line zero", "", 0, "", true},
		{"empty text has no line one", "", 1, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := LineText(tc.src, tc.line)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("LineText(%q, %d) = (%q, %v), want (%q, %v)",
					tc.src, tc.line, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestEscapePointerSegment_RoundTrip(t *testing.T) {
	for _, seg := range []string{"plain", "a/b", "a~b", "~1", "/", "~", "a~/b"} {
		esc := EscapePointerSegment(seg)
		if got := UnescapePointerSegment(esc); got != seg {
			t.Errorf("round trip %q -> %q -> %q", seg, esc, got)
		}
	}
	if got := EscapePointerSegment("a/b~c"); got != "a~1b~0c" {
		t.Errorf("escape order wrong: got %q, want %q", got, "a~1b~0c")
	}
}

func TestIsKeyLine(t *testing.T) {
	text := "name: x\nspec:\n  \n"
	cases := []struct {
		name string
		pos  protocol.Position
		want bool
	}{
		{"before colon", protocol.Position{Line: 0, Character: 2}, true},
		{"after colon", protocol.Position{Line: 0, Character: 6}, false},
		{"blank line", protocol.Position{Line: 2, Character: 2}, true},
		{"line past end of document", protocol.Position{Line: 9, Character: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isKeyLine(text, tc.pos); got != tc.want {
				t.Errorf("isKeyLine(%v) = %v, want %v", tc.pos, got, tc.want)
			}
		})
	}
}
