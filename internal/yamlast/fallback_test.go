package yamlast

import "testing"

func TestInferPathByIndent_NestedMapping(t *testing.T) {
	text := `spec:
  template:
    metadata:
      labels:
        `
	got := inferPathByIndent(text, 4)
	want := "/spec/template/metadata/labels"
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestInferPathByIndent_SequenceItemMapping(t *testing.T) {
	text := `containers:
  - name: web
    `
	got := inferPathByIndent(text, 2)
	want := "/containers/0"
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestForCursor_RecoversFromBrokenLine(t *testing.T) {
	text := "name: Alice\nage: \"thirty"
	p := ForCursor(Parse([]byte(text)), 1)
	if p.File == nil || len(p.File.Docs) == 0 {
		t.Fatalf("expected recovered AST, got nil docs")
	}
}

func TestForCursor_CleanParseReturnsSamePointer(t *testing.T) {
	p := Parse([]byte("name: Alice\n"))
	if p.Err != nil {
		t.Fatalf("unexpected parse error: %v", p.Err)
	}
	if got := ForCursor(p, 0); got != p {
		t.Errorf("ForCursor returned a new Parsed for a clean parse")
	}
}

func TestForCursor_BrokenParseReturnsBlankedFallback(t *testing.T) {
	text := "name: Alice\nage: \"thirty"
	p := Parse([]byte(text))
	if p.Err == nil {
		t.Fatalf("expected parse error for broken text")
	}
	got := ForCursor(p, 1)
	if got == p {
		t.Fatalf("expected a distinct fallback Parsed")
	}
	if got.File == nil || len(got.File.Docs) == 0 {
		t.Fatalf("expected recovered AST, got nil docs")
	}
	if p.Text != text {
		t.Errorf("original Parsed mutated: %q", p.Text)
	}
}
