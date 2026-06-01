package completion_test

import (
	"strings"
	"testing"

	"github.com/home-operations/yayamlls/internal/completion"
	"github.com/santhosh-tekuri/jsonschema/v6"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func compile(t *testing.T, body string) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("mem://t.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("mem://t.json")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func TestCompletion_ValuesAtValuePosition(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {
			"tier": {"oneOf": [{"enum": ["bronze","silver"]},{"const":"gold"}]},
			"enabled": {"type": "boolean"}
		}
	}`)
	text := "tier: \nenabled: \n"
	// cursor right after "tier: "
	list := completion.At(text, protocol.Position{Line: 0, Character: 6}, sch)
	if list == nil {
		t.Fatal("nil list for tier value")
	}
	got := map[string]bool{}
	for _, it := range list.Items {
		got[it.Label] = true
	}
	t.Logf("tier items: %v", got)
	for _, w := range []string{"bronze", "silver", "gold"} {
		if !got[w] {
			t.Errorf("tier missing %q: %v", w, got)
		}
	}
	list2 := completion.At(text, protocol.Position{Line: 1, Character: 9}, sch)
	if list2 == nil {
		t.Fatal("nil list for enabled value")
	}
	g2 := map[string]bool{}
	for _, it := range list2.Items {
		g2[it.Label] = true
	}
	t.Logf("enabled items: %v", g2)
	if !g2["true"] || !g2["false"] {
		t.Errorf("enabled missing booleans: %v", g2)
	}
}
