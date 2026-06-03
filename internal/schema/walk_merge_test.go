package schema

import "testing"

func TestStep_RefMergesSiblingProperties(t *testing.T) {
	sch := compileSchema(t, `{
		"$defs": { "base": { "type": "object", "properties": { "fromRef": {"type": "string"} } } },
		"type": "object",
		"properties": {
			"spec": {
				"$ref": "#/$defs/base",
				"description": "spec desc",
				"properties": { "localOnly": {"type": "string"} }
			}
		}
	}`)
	spec := Resolve(sch, "/spec")
	if spec == nil {
		t.Fatal("spec nil")
	}
	if spec.Description != "spec desc" {
		t.Errorf("lost sibling description: %q", spec.Description)
	}
	props := Properties(spec)
	for _, want := range []string{"fromRef", "localOnly"} {
		if _, ok := props[want]; !ok {
			t.Errorf("missing %q; got %v", want, keysOf(props))
		}
	}
	// Navigation into a key that lives only on the $ref target must work too.
	if Resolve(sch, "/spec/fromRef") == nil {
		t.Error("could not navigate into $ref-only key /spec/fromRef")
	}
	if Resolve(sch, "/spec/localOnly") == nil {
		t.Error("could not navigate into sibling key /spec/localOnly")
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
