package folding

import (
	"testing"

	"github.com/home-operations/yayamlls/internal/yamlast"
)

func TestRanges_MultilineMappingAndSequence(t *testing.T) {
	text := `spec:
  containers:
    - name: web
      image: nginx
  replicas: 3
`
	got := Ranges(yamlast.Parse([]byte(text)))
	if len(got) == 0 {
		t.Fatalf("expected at least one folding range, got 0")
	}
	covers := func(start, end uint32) bool {
		for _, r := range got {
			if r.StartLine == start && r.EndLine >= end {
				return true
			}
		}
		return false
	}
	// Whole spec mapping spans lines 0..4 (0-indexed).
	if !covers(0, 4) {
		t.Errorf("expected fold covering top-level spec mapping; got %+v", got)
	}
	// containers sequence starts on line 2 (where `- name` lives).
	if !covers(2, 3) {
		t.Errorf("expected fold for containers sequence; got %+v", got)
	}
}

func TestRanges_NoRangeForSingleLineDoc(t *testing.T) {
	if got := Ranges(yamlast.Parse([]byte("name: x\n"))); len(got) != 0 {
		t.Errorf("expected zero ranges for single-line doc, got %+v", got)
	}
}

func TestRanges_EmptyAndCommentOnlyDocs(t *testing.T) {
	// Empty docs have a nil Body; walking them must not panic.
	for _, text := range []string{"", "# just a comment\n", "---\n# c\n---\n"} {
		if got := Ranges(yamlast.Parse([]byte(text))); len(got) != 0 {
			t.Errorf("expected zero ranges for %q, got %+v", text, got)
		}
	}
}
