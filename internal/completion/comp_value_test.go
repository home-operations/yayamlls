package completion_test

import (
	"strings"
	"testing"

	"github.com/home-operations/yayamlls/internal/completion"
	"github.com/home-operations/yayamlls/internal/yamlast"
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
	list := completion.At(yamlast.Parse([]byte(text)), protocol.Position{Line: 0, Character: 6}, sch, completion.Options{})
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
	list2 := completion.At(yamlast.Parse([]byte(text)), protocol.Position{Line: 1, Character: 9}, sch, completion.Options{})
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

func TestCompletion_EnumValuesAreQuotedWhenNeeded(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {"mode": {"enum": ["plain", "true", "1.0", "a: b", "", 7, true]}}
	}`)
	tests := []struct {
		label string
		want  *string // nil: no explicit InsertText, label inserts as-is
	}{
		{"plain", nil},
		{"true", new(`"true"`)},
		{"1.0", new(`"1.0"`)},
		{"a: b", new(`"a: b"`)},
		{"", new(`""`)},
	}
	list := completion.At(yamlast.Parse([]byte("mode: ")), protocol.Position{Line: 0, Character: 6}, sch, completion.Options{})
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got := itemByLabel(t, list, tt.label).InsertText
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("InsertText = %v, want %v", strOrNil(got), strOrNil(tt.want))
			}
		})
	}
	t.Run("non-string scalars stay bare", func(t *testing.T) {
		for _, l := range []string{"7", "true"} {
			for _, it := range list.Items {
				if it.Label == l && it.InsertText != nil && *it.InsertText != l && *it.InsertText != `"true"` {
					t.Errorf("label %q inserts %q", l, *it.InsertText)
				}
			}
		}
	})
	t.Run("leading space after colon", func(t *testing.T) {
		l := completion.At(yamlast.Parse([]byte("mode:")), protocol.Position{Line: 0, Character: 5}, sch, completion.Options{})
		if got := itemByLabel(t, l, "true").InsertText; got == nil || *got != ` "true"` {
			t.Errorf("InsertText = %v, want ` \"true\"`", strOrNil(got))
		}
	})
}
