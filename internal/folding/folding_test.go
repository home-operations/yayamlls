package folding

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func formatRanges(ranges []protocol.FoldingRange) string {
	var b strings.Builder
	for i, r := range ranges {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "[L%d-L%d]", r.StartLine, r.EndLine)
	}
	return b.String()
}

func assertRanges(t *testing.T, got, want []protocol.FoldingRange) {
	t.Helper()
	slices.SortFunc(got, func(a, b protocol.FoldingRange) int {
		if c := cmp.Compare(a.StartLine, b.StartLine); c != 0 {
			return c
		}
		return cmp.Compare(a.EndLine, b.EndLine)
	})
	if !slices.EqualFunc(got, want, func(a, b protocol.FoldingRange) bool {
		return a.StartLine == b.StartLine && a.EndLine == b.EndLine
	}) {
		t.Errorf("ranges mismatch:\n got: %s\nwant: %s", formatRanges(got), formatRanges(want))
	}
}

func TestRanges_MultilineMapping(t *testing.T) {
	text := `apiVersion: "v1"
kind: "Namespace"
metadata:
  name: "test"
`
	got := Ranges(yamlast.Parse([]byte(text)))
	assertRanges(t, got, []protocol.FoldingRange{
		{StartLine: 0, EndLine: 3}, // root
		{StartLine: 2, EndLine: 3}, // "metadata:"
	})
}

func TestRanges_MultilineMappingAndSequence(t *testing.T) {
	text := `spec:
  containers:
    - name: web
      image: nginx
  replicas: 3
`
	got := Ranges(yamlast.Parse([]byte(text)))
	assertRanges(t, got, []protocol.FoldingRange{
		{StartLine: 0, EndLine: 4}, // root
		{StartLine: 1, EndLine: 3}, // "containers:"
		{StartLine: 2, EndLine: 3}, // "- name: web"
	})
}

func TestRanges_MultilineFlowMapping(t *testing.T) {
	text := `{
  apiVersion: "v1",
  kind: "Namespace",
  metadata: {
    name: "test",
  },
}
`
	got := Ranges(yamlast.Parse([]byte(text)))
	assertRanges(t, got, []protocol.FoldingRange{
		{StartLine: 0, EndLine: 6}, // root
		{StartLine: 3, EndLine: 5}, // "metadata: {"
	})
}

func TestRanges_MultilineFlowMappingNested(t *testing.T) {
	text := `{
  apiVersion: "v1",
  kind: "Test",
  metadata: {
    name: "test",
  },
  spec: {
    foobar: {
    },
  },
}
`
	got := Ranges(yamlast.Parse([]byte(text)))
	assertRanges(t, got, []protocol.FoldingRange{
		{StartLine: 0, EndLine: 10}, // root
		{StartLine: 3, EndLine: 5},  // "metadata:"
		{StartLine: 6, EndLine: 9},  // "spec:"
		{StartLine: 7, EndLine: 8},  // "foobar:"
	})
}

func TestRanges_MultilineFlowMappingAndSequence(t *testing.T) {
	text := `{
  apiVersion: "kustomize.config.k8s.io/v1beta1",
  kind: "Kustomization",
  namespace: "test",
  resources: [
    "test1.yaml",
    "test2.yaml"
  ],
}
`
	got := Ranges(yamlast.Parse([]byte(text)))

	assertRanges(t, got, []protocol.FoldingRange{
		{StartLine: 0, EndLine: 8}, // root
		{StartLine: 4, EndLine: 7}, // "resources: ["
	})
}

func TestRanges_NoRangeForSingleLineDoc(t *testing.T) {
	if got := Ranges(yamlast.Parse([]byte("name: x\n"))); len(got) != 0 {
		t.Errorf("expected zero ranges for single-line doc, got %s", formatRanges(got))
	}
}

func TestRanges_SequenceWithMappings(t *testing.T) {
	text := `list:
  - x: 1
    y: 2
  - x: 1
  - x: 1.1
    y: 6.3
    z: 2.0
`
	got := Ranges(yamlast.Parse([]byte(text)))
	assertRanges(t, got, []protocol.FoldingRange{
		{StartLine: 0, EndLine: 6}, // root
		{StartLine: 1, EndLine: 2}, // first item
		{StartLine: 4, EndLine: 6}, // third item
	})
}

func TestRanges_EmptyAndCommentOnlyDocs(t *testing.T) {
	// Empty docs have a nil Body; walking them must not panic.
	for _, text := range []string{"", "# just a comment\n", "---\n# c\n---\n"} {
		if got := Ranges(yamlast.Parse([]byte(text))); len(got) != 0 {
			t.Errorf("expected zero ranges for %q, got %s", text, formatRanges(got))
		}
	}
}
