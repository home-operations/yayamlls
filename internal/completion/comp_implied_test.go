package completion_test

import (
	"testing"

	"github.com/home-operations/yayamlls/internal/completion"
	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// A boolean field without an enum still offers true/false; a default equal
// to one of them must not appear twice.
func TestCompletion_BooleanWithDefaultDeduplicated(t *testing.T) {
	sch := compile(t, `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"enabled": {"type": "boolean", "default": true}
		}
	}`)
	body := "enabled: \n"
	list := completion.At(yamlast.Parse([]byte(body)), protocol.Position{Line: 0, Character: 9}, sch, completion.Options{})
	if list == nil {
		t.Fatal("expected completions at boolean value position")
	}
	labels := map[string]int{}
	for _, it := range list.Items {
		labels[it.Label]++
	}
	if labels["true"] != 1 || labels["false"] != 1 {
		t.Errorf("expected exactly one true and one false, got %v", labels)
	}
}

// A non-boolean default is offered as a value completion.
func TestCompletion_DefaultValueOffered(t *testing.T) {
	sch := compile(t, `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"level": {"type": "string", "default": "info"}
		}
	}`)
	body := "level: \n"
	list := completion.At(yamlast.Parse([]byte(body)), protocol.Position{Line: 0, Character: 7}, sch, completion.Options{})
	if list == nil {
		t.Fatal("expected completions at value position")
	}
	for _, it := range list.Items {
		if it.Label == "info" {
			return
		}
	}
	t.Errorf("default value not offered; items = %v", list.Items)
}
