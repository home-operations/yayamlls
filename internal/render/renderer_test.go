package render_test

import (
	"testing"

	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/yamlast"
)

func TestMatchesKind_RequiresGroupBoundary(t *testing.T) {
	doc := func(group string) *render.SourceDocument {
		return &render.SourceDocument{Kind: "HelmRelease", APIGroup: group}
	}
	const group = "helm.toolkit.fluxcd.io"

	if !render.MatchesKind(doc("helm.toolkit.fluxcd.io/v2"), "HelmRelease", group) {
		t.Error("versioned group should match")
	}
	if !render.MatchesKind(doc("helm.toolkit.fluxcd.io"), "HelmRelease", group) {
		t.Error("bare group should match")
	}
	if render.MatchesKind(doc("helm.toolkit.fluxcd.iox/v1"), "HelmRelease", group) {
		t.Error("group sharing only a prefix must not match")
	}
}

func TestAnalyzeDocument_Namespace(t *testing.T) {
	cases := []struct {
		name, text, want string
	}{
		{"set", "apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n  namespace: ns-a\n", "ns-a"},
		{"omitted", "apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := render.AnalyzeDocument("file:///x.yaml", "/x.yaml", yamlast.Parse([]byte(c.text)))
			if doc == nil {
				t.Fatal("AnalyzeDocument returned nil")
			}
			if doc.Namespace != c.want {
				t.Errorf("Namespace = %q, want %q", doc.Namespace, c.want)
			}
		})
	}
}
