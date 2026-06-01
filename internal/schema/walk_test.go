package schema

import (
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func compileSchema(t *testing.T, body string) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("mem://test.json", doc); err != nil {
		t.Fatalf("add resource: %v", err)
	}
	sch, err := c.Compile("mem://test.json")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return sch
}

func TestResolve_PatternProperties(t *testing.T) {
	sch := compileSchema(t, `{
		"type": "object",
		"patternProperties": {
			"^x-": {"type": "object", "properties": {"name": {"type": "string"}}}
		}
	}`)
	target := Resolve(sch, "/x-foo")
	if target == nil {
		t.Fatal("expected patternProperties match for /x-foo, got nil")
	}
	if _, ok := Properties(target)["name"]; !ok {
		t.Errorf("expected resolved patternProperties schema to expose 'name'")
	}
}

func TestResolve_RecursiveRef(t *testing.T) {
	sch := compileSchema(t, `{
		"$id": "mem://test.json",
		"type": "object",
		"properties": {
			"child": {"$ref": "#"},
			"label": {"type": "string"}
		}
	}`)
	// Step through one level of recursion and confirm the child still
	// resolves the recursive object's own properties.
	target := Resolve(sch, "/child")
	if target == nil {
		t.Fatal("expected /child to resolve via $ref")
	}
	if _, ok := Properties(target)["label"]; !ok {
		t.Errorf("recursive $ref did not resolve to the object's properties")
	}
}
