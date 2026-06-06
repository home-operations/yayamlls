package diagnostics_test

import (
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
