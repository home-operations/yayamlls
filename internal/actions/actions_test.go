package actions_test

import (
	"bytes"
	"testing"

	"github.com/home-operations/yayamlls/internal/actions"
	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/santhosh-tekuri/jsonschema/v5"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestEnumQuickFix(t *testing.T) {
	const schemaJSON = `{
	  "$schema": "https://json-schema.org/draft/2020-12/schema",
	  "type": "object",
	  "properties": {"kind": {"enum": ["Pod", "Service", "Deployment"]}}
	}`
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	c.ExtractAnnotations = true
	if err := c.AddResource("mem://schema.json", bytes.NewReader([]byte(schemaJSON))); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("mem://schema.json")
	if err != nil {
		t.Fatal(err)
	}

	diag := protocol.Diagnostic{
		Range:   protocol.Range{Start: protocol.Position{Line: 0, Character: 6}, End: protocol.Position{Line: 0, Character: 9}},
		Message: "enum-violation",
		Data: diagnostics.CauseData{
			Kind:             "enum",
			InstanceLocation: "/kind",
		},
	}
	got := actions.Compute("file:///tmp/x.yaml", "kind: Pod\n", sch, []protocol.Diagnostic{diag})
	if len(got) != 3 {
		t.Fatalf("expected 3 actions (one per enum value), got %d", len(got))
	}
	titles := make(map[string]bool, len(got))
	for _, a := range got {
		titles[a.Title] = true
	}
	for _, want := range []string{"Replace with Pod", "Replace with Service", "Replace with Deployment"} {
		if !titles[want] {
			t.Errorf("missing action %q in %v", want, titles)
		}
	}
	// First action should carry a workspace edit on the same URI.
	edit := got[0].Edit
	if edit == nil || edit.Changes == nil {
		t.Fatal("missing WorkspaceEdit")
	}
	if _, ok := edit.Changes["file:///tmp/x.yaml"]; !ok {
		t.Errorf("workspace edit doesn't target the document URI")
	}
}

func TestNonEnumDiagnosticHasNoActions(t *testing.T) {
	diag := protocol.Diagnostic{
		Data: diagnostics.CauseData{Kind: "type", InstanceLocation: "/foo"},
	}
	if got := actions.Compute("file:///x", "", nil, []protocol.Diagnostic{diag}); len(got) != 0 {
		t.Errorf("expected 0 actions, got %d", len(got))
	}
}

func TestSuppressAction(t *testing.T) {
	source := diagnostics.Source
	diag := protocol.Diagnostic{
		Range:  protocol.Range{Start: protocol.Position{Line: 1, Character: 2}, End: protocol.Position{Line: 1, Character: 5}},
		Source: &source,
		Data:   diagnostics.CauseData{Kind: "type", InstanceLocation: "/spec"},
	}
	const text = "spec:\n  foo: bar\n"
	got := actions.Compute("file:///x.yaml", text, nil, []protocol.Diagnostic{diag})
	if len(got) != 1 {
		t.Fatalf("expected 1 suppress action, got %d", len(got))
	}
	if got[0].Title != "Suppress this diagnostic" {
		t.Errorf("unexpected title %q", got[0].Title)
	}
	edits := got[0].Edit.Changes["file:///x.yaml"]
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	// The directive is inserted on its own line above the offending line,
	// matching its two-space indent.
	if want := "  " + diagnostics.DisableLineComment() + "\n"; edits[0].NewText != want {
		t.Errorf("NewText = %q, want %q", edits[0].NewText, want)
	}
	if at := (protocol.Position{Line: 1, Character: 0}); edits[0].Range.Start != at || edits[0].Range.End != at {
		t.Errorf("edit not a zero-width insert at line start: %+v", edits[0].Range)
	}
}

func TestNoSuppressWithoutYayamllsSource(t *testing.T) {
	other := "other-linter"
	diag := protocol.Diagnostic{Source: &other}
	if got := actions.Compute("file:///x", "a: b\n", nil, []protocol.Diagnostic{diag}); len(got) != 0 {
		t.Errorf("expected 0 actions for foreign source, got %d", len(got))
	}
}
