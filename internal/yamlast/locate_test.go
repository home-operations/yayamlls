package yamlast

import (
	"testing"
	"time"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestLocateRange_SpansMultilineMapping(t *testing.T) {
	text := `spec:
  containers:
    - name: web
      image: nginx
`
	parsed := Parse([]byte(text))
	if parsed.Err != nil {
		t.Fatalf("parse: %v", parsed.Err)
	}
	docs := parsed.Docs()
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	// /spec/containers resolves to the sequence node; its first token
	// is the `-` on line 2.
	r := LocateRange(docs[0], "/spec/containers", text)
	if r.Start.Line != 2 {
		t.Errorf("start line = %d, want 2 (the `-` line)", r.Start.Line)
	}
	if r.End.Line < 3 {
		t.Errorf("end line = %d, want >= 3", r.End.Line)
	}
}

func TestLocateRange_ScalarHasNonZeroWidth(t *testing.T) {
	text := "age: thirty\n"
	parsed := Parse([]byte(text))
	r := LocateRange(parsed.Docs()[0], "/age", text)
	want := protocol.Position{Line: 0, Character: 5 + 6}
	if r.End != want {
		t.Errorf("end = %+v, want %+v", r.End, want)
	}
}

func TestLocateRange_NumericMappingKeyNotTreatedAsIndex(t *testing.T) {
	// 8080 is a mapping key, not an array index. The range must land on the
	// value (line 1), not fall back to the whole document body (line 0).
	text := "ports:\n  8080: tcp\n"
	parsed := Parse([]byte(text))
	r := LocateRange(parsed.Docs()[0], "/ports/8080", text)
	if r.Start.Line != 1 {
		t.Errorf("numeric-key range start line = %d, want 1 (got whole-doc fallback?)", r.Start.Line)
	}
}

func TestLocateRange_KeyWithDotAndSlash(t *testing.T) {
	// app.kubernetes.io/name is a single key; the '/' is JSON-Pointer escaped
	// as ~1. It must resolve to the value (line 2), not the document body.
	text := "metadata:\n  annotations:\n    app.kubernetes.io/name: web\n"
	parsed := Parse([]byte(text))
	r := LocateRange(parsed.Docs()[0], "/metadata/annotations/app.kubernetes.io~1name", text)
	if r.Start.Line != 2 {
		t.Errorf("dotted/slash-key range start line = %d, want 2", r.Start.Line)
	}
}

func TestUTF16Position_TranslatesRuneColumnsToUTF16(t *testing.T) {
	// goccy reports the value token at rune column 7 (1-based): the six
	// preceding runes are 😀,k,e,y,:,space. 😀 is two UTF-16 code units, so
	// the LSP character is 7, not the 6 a rune count would give.
	line := "😀key: value"
	if got := UTF16Position(line, 1, 7); got.Character != 7 {
		t.Errorf("character = %d, want 7", got.Character)
	}
}

func TestUTF16Len_CountsCodeUnits(t *testing.T) {
	if got := UTF16Len("😀ab"); got != 4 { // 😀=2, a=1, b=1
		t.Errorf("UTF16Len = %d, want 4", got)
	}
}

func TestLocate_AliasesAndMerges(t *testing.T) {
	src := "base: &b\n  port: 80\nsvc:\n  <<: *b\n  name: x\nal: *b\nseq:\n  <<: [*b]\nown:\n  <<: *b\n  port: 1\n"
	doc := Parse([]byte(src)).Docs()[0]

	tests := []struct {
		name string
		ptr  string
		line uint32
	}{
		{"through anchor definition", "/base/port", 1},
		{"merged key points at merge alias", "/svc/port", 3},
		{"explicit key beats merge", "/svc/name", 4},
		{"merge from sequence of aliases", "/seq/port", 7},
		{"alias value descent points at alias", "/al/port", 5},
		{"explicit key shadows merge", "/own/port", 10},
		{"missing leaf anchors on deepest ancestor", "/svc/missing", 3},
		{"missing under plain mapping anchors on parent", "/own/missing/deeper", 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LocateRange(doc, tt.ptr, src).Start.Line; got != tt.line {
				t.Errorf("LocateRange(%q) line = %d, want %d", tt.ptr, got, tt.line)
			}
		})
	}

	t.Run("LocateKey on merged key", func(t *testing.T) {
		r, ok := LocateKey(doc, "/svc", "port", src)
		if !ok || r.Start.Line != 3 {
			t.Errorf("LocateKey = %v, %v; want line 3", r, ok)
		}
	})
	t.Run("LocateKey explicit key", func(t *testing.T) {
		r, ok := LocateKey(doc, "/svc", "name", src)
		if !ok || r.Start.Line != 4 {
			t.Errorf("LocateKey = %v, %v; want line 4", r, ok)
		}
	})
}

func TestLocate_SelfReferentialMergeTerminates(t *testing.T) {
	src := "a: &a\n  <<: *a\n  k: 1\n"
	doc := Parse([]byte(src)).Docs()[0]
	_ = LocateRange(doc, "/a/nope", src)
}

func TestLocate_RepeatedMergeSourcesStayLinear(t *testing.T) {
	src := "a: &a {<<: [*a, *a]}\n"
	doc := Parse([]byte(src)).Docs()[0]
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = LocateRange(doc, "/a/nope", src)
		_, _ = LocateKey(doc, "/a", "nope", src)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("lookup through repeated merge sources did not finish")
	}
}
