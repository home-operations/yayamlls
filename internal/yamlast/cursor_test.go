package yamlast

import (
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Completion inside a sequence of mappings must target the enclosing item
// mapping for every key, not just the first one on the `- ` line. A cursor
// on an indented continuation line (a sibling key) used to attach to the
// previous entry's value because selection was offset-based; it is now
// indentation-aware.
func TestLocateCursor_SequenceItemContinuation(t *testing.T) {
	cases := []struct {
		name        string
		text        string
		line, char  uint32
		wantPointer string
		wantIsKey   bool
	}{
		{"first key on dash line", "containers:\n  - \n", 1, 4, "/containers/0", true},
		{"continuation key (blank)", "containers:\n  - name: app\n    \n", 2, 4, "/containers/0", true},
		{"continuation key (typing)", "containers:\n  - name: app\n    im\n", 2, 6, "/containers/0", true},
		{"new dash item", "containers:\n  - name: app\n  - \n", 2, 4, "/containers/1", true},
		{"inline value position", "containers:\n  - name: \n", 1, 9, "/containers/0/name", false},
		{"dedent to outer mapping", "spec:\n  template:\n    foo: bar\n  \n", 3, 2, "/spec", true},
		{"nested sequence continuation", "spec:\n  containers:\n    - name: web\n      \n", 3, 6, "/spec/containers/0", true},
	}
	for _, c := range cases {
		parsed := ParseForCursor(c.text, int(c.line))
		ctx := LocateCursor(parsed, c.text, protocol.Position{Line: c.line, Character: c.char})
		if ctx.Pointer != c.wantPointer || ctx.IsKey != c.wantIsKey {
			t.Errorf("%s: pointer=%q isKey=%v, want pointer=%q isKey=%v",
				c.name, ctx.Pointer, ctx.IsKey, c.wantPointer, c.wantIsKey)
		}
	}
}

func TestPathToPointer(t *testing.T) {
	cases := map[string]string{
		"$":            "",
		"$.a.b":        "/a/b",
		"$.items[0].x": "/items/0/x",
		"$.'a.b'.c":    "/a.b/c", // quoted key keeps its dot as one segment
		"$.a/b":        "/a~1b",  // slash in a key is JSON-Pointer escaped
		"$.'x/y'":      "/x~1y",  // quoted key containing a slash
	}
	for in, want := range cases {
		if got := pathToPointer(in); got != want {
			t.Errorf("pathToPointer(%q) = %q, want %q", in, got, want)
		}
	}
}
