package symbols

import (
	"testing"

	"github.com/home-operations/yayamlls/internal/yamlast"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestOutline_KubernetesManifest(t *testing.T) {
	text := `apiVersion: v1
kind: Pod
metadata:
  name: web
  namespace: default
spec:
  containers:
    - name: app
      image: nginx
`
	got := Outline(yamlast.Parse([]byte(text)))
	if len(got) != 1 {
		t.Fatalf("expected 1 doc symbol, got %d", len(got))
	}
	if got[0].Name != "Pod/web.default" {
		t.Errorf("doc label = %q, want Pod/web.default", got[0].Name)
	}
	if len(got[0].Children) != 4 {
		t.Errorf("expected 4 top-level children, got %d (%v)",
			len(got[0].Children), childNames(got[0].Children))
	}
}

func TestOutline_NonKubernetesDoc(t *testing.T) {
	got := Outline(yamlast.Parse([]byte("name: Alice\nage: 30\n")))
	if len(got) != 1 {
		t.Fatalf("expected 1 doc symbol, got %d", len(got))
	}
	if got[0].Name != "document" {
		t.Errorf("doc label = %q, want 'document'", got[0].Name)
	}
	names := childNames(got[0].Children)
	if len(names) != 2 || names[0] != "name" || names[1] != "age" {
		t.Errorf("children = %v, want [name age]", names)
	}
}

func childNames(syms []protocol.DocumentSymbol) []string {
	out := make([]string, len(syms))
	for i, s := range syms {
		out[i] = s.Name
	}
	return out
}

func within(outer, inner protocol.Range) bool {
	before := func(a, b protocol.Position) bool {
		return a.Line < b.Line || (a.Line == b.Line && a.Character <= b.Character)
	}
	return before(outer.Start, inner.Start) && before(inner.End, outer.End)
}

func TestOutline_EntryRangeContainsChildrenAndKey(t *testing.T) {
	got := Outline(yamlast.Parse([]byte("spec:\n  replicas: 1\n  image: nginx\n\"\": x\n")))
	top := got[0].Children
	spec := top[0]
	if !within(spec.Range, spec.SelectionRange) {
		t.Errorf("selectionRange %v not inside range %v", spec.SelectionRange, spec.Range)
	}
	if len(spec.Children) != 2 {
		t.Fatalf("children = %v", childNames(spec.Children))
	}
	for _, c := range spec.Children {
		if !within(spec.Range, c.Range) {
			t.Errorf("child %q range %v outside parent %v", c.Name, c.Range, spec.Range)
		}
	}
	if top[1].Name != `""` {
		t.Errorf("empty key name = %q, want \"\"", top[1].Name)
	}
}
