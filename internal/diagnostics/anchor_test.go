package diagnostics_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/yamlast"
)

const nestedRequiredSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {
		"spec": {
			"type": "object",
			"properties": {"replicas": {"type": "integer"}},
			"required": ["replicas"],
			"additionalProperties": false
		}
	}
}`

// A required violation reports against the parent object, whose range starts
// at its first child; the diagnostic must steer to the owning key so the
// squiggle lands on `spec`, not on an unrelated nested line.
func TestValidateDoc_RequiredAnchorsOnOwningKey(t *testing.T) {
	sch := compileInlineSchema(t, nestedRequiredSchema)
	body := "name: x\nspec:\n  image: nginx\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	var found bool
	for _, d := range diags {
		if !strings.Contains(d.Message, "replicas") {
			continue
		}
		found = true
		if d.Range.Start.Line != 1 {
			t.Errorf("required diagnostic anchored on line %d, want 1 (the spec: key)", d.Range.Start.Line)
		}
	}
	if !found {
		t.Fatalf("no diagnostic mentioned the missing required property; got %v", diags)
	}
}

// additionalProperties also reports against the parent object; the diagnostic
// must anchor on the offending key's own line.
func TestValidateDoc_AdditionalPropertyAnchorsOnOffendingKey(t *testing.T) {
	sch := compileInlineSchema(t, nestedRequiredSchema)
	body := "spec:\n  replicas: 1\n  bogus: true\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	var found bool
	for _, d := range diags {
		if !strings.Contains(d.Message, "bogus") {
			continue
		}
		found = true
		if d.Range.Start.Line != 2 {
			t.Errorf("additionalProperties diagnostic anchored on line %d, want 2 (the bogus: key)", d.Range.Start.Line)
		}
	}
	if !found {
		t.Fatalf("no diagnostic mentioned the unknown property; got %v", diags)
	}
}

// A value supplied by a `<<: *anchor` merge or an alias must anchor on the
// referencing line, not fall back to the whole document.
func TestValidateDoc_MergedAndAliasedValuesAnchorOnReference(t *testing.T) {
	sch := compileInlineSchema(t, `{
		"type": "object",
		"properties": {
			"svc": {"type": "object", "properties": {"port": {"type": "string"}}},
			"al": {"type": "object", "properties": {"port": {"type": "string"}}}
		}
	}`)
	body := "base: &b\n  port: 80\nsvc:\n  <<: *b\n  name: x\nal: *b\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	lines := make([]uint32, 0, len(diags))
	for _, d := range diags {
		lines = append(lines, d.Range.Start.Line)
	}
	slices.Sort(lines)
	if len(lines) != 2 || lines[0] != 3 || lines[1] != 5 {
		t.Errorf("diagnostic start lines = %v, want [3 5]", lines)
	}
}
