package symbols

import (
	"testing"

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
	got := Outline(text)
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
	got := Outline("name: Alice\nage: 30\n")
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
