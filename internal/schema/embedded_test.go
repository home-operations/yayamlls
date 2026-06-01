package schema

import (
	"testing"
)

func TestEmbeddedYamllsSchema_LoadsAndCompiles(t *testing.T) {
	sch, err := NewStore().Get(EmbeddedYamllsSchemaURL, "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	good := map[string]any{
		"catalog": true,
		"kubernetes": map[string]any{
			"schemaUrl": "https://example.com/{kindLower}.json",
		},
	}
	if err := sch.Validate(good); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}

	bad := map[string]any{"schmas": map[string]any{"a": "b"}}
	if err := sch.Validate(bad); err == nil {
		t.Errorf("typo'd key 'schmas' should be rejected")
	}
}

func TestResolver_RecognizesYamllsConfigPath(t *testing.T) {
	r := NewResolver()
	if got := r.Resolve("", "/repo/.yayamlls.yaml"); got != EmbeddedYamllsSchemaURL {
		t.Errorf("got %q, want %q", got, EmbeddedYamllsSchemaURL)
	}
	if got := r.Resolve("", "/repo/other.yaml"); got == EmbeddedYamllsSchemaURL {
		t.Errorf("unrelated file should NOT match the embedded schema")
	}
}
