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
			"schemaUrl": "https://example.com/{kind}.json",
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

func TestEmbeddedYamllsSchema_IgnoreRequired(t *testing.T) {
	sch, err := NewStore().Get(EmbeddedYamllsSchemaURL, "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	fluxMatch := map[string]any{"kind": "Kustomization", "group": "kustomize.toolkit.fluxcd.io"}
	tests := []struct {
		name  string
		rule  map[string]any
		valid bool
	}{
		{name: "kind and group", rule: map[string]any{"match": fluxMatch, "paths": []any{"/spec/prune"}}, valid: true},
		{name: "core group", rule: map[string]any{
			"match": map[string]any{"kind": "ConfigMap"}, "paths": []any{"/data"},
		}, valid: true},
		{name: "missing paths", rule: map[string]any{"match": fluxMatch}},
		{name: "empty paths", rule: map[string]any{"match": fluxMatch, "paths": []any{}}},
		{name: "path is not a pointer", rule: map[string]any{"match": fluxMatch, "paths": []any{"spec.prune"}}},
		{name: "missing match", rule: map[string]any{"paths": []any{"/spec/prune"}}},
		{name: "match without kind", rule: map[string]any{
			"match": map[string]any{"group": "kustomize.toolkit.fluxcd.io"}, "paths": []any{"/spec/prune"},
		}},
		{name: "unknown match key", rule: map[string]any{
			"match": map[string]any{"kind": "Kustomization", "version": "v1"}, "paths": []any{"/spec/prune"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sch.Validate(map[string]any{"ignoreRequired": []any{tt.rule}})
			if tt.valid && err != nil {
				t.Errorf("valid rule rejected: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("invalid rule accepted")
			}
		})
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
