package actions_test

import (
	"bytes"
	"testing"

	"github.com/home-operations/yayamlls/internal/actions"
	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/santhosh-tekuri/jsonschema/v6"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestEnumQuickFix(t *testing.T) {
	const schemaJSON = `{
	  "$schema": "https://json-schema.org/draft/2020-12/schema",
	  "type": "object",
	  "properties": {"kind": {"enum": ["Pod", "Service", "Deployment"]}}
	}`
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(schemaJSON)))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("mem://schema.json", doc); err != nil {
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
	// The directive trails the offending line so it suppresses that very line
	// without shifting line numbers.
	if want := " " + diagnostics.DisableLineComment(); edits[0].NewText != want {
		t.Errorf("NewText = %q, want %q", edits[0].NewText, want)
	}
	if at := (protocol.Position{Line: 1, Character: uint32(len("  foo: bar"))}); edits[0].Range.Start != at || edits[0].Range.End != at {
		t.Errorf("edit not a zero-width append at line end: %+v", edits[0].Range)
	}
}

func TestSuppressActionCommentLineFallsBackAbove(t *testing.T) {
	source := diagnostics.Source
	// The diagnostic anchors on a comment-only line (no code to trail), so the
	// directive goes on its own line above.
	diag := protocol.Diagnostic{
		Range:  protocol.Range{Start: protocol.Position{Line: 0}},
		Source: &source,
	}
	const text = "# yaml-language-server: $schema=x\nname: a\n"
	got := actions.Compute("file:///x.yaml", text, nil, []protocol.Diagnostic{diag})
	if len(got) != 1 {
		t.Fatalf("expected 1 action, got %d", len(got))
	}
	e := got[0].Edit.Changes["file:///x.yaml"][0]
	if want := diagnostics.DisableLineComment() + "\n"; e.NewText != want {
		t.Errorf("NewText = %q, want %q", e.NewText, want)
	}
	if at := (protocol.Position{Line: 0, Character: 0}); e.Range.Start != at {
		t.Errorf("expected insert at line start, got %+v", e.Range.Start)
	}
}

func TestNoSuppressWithoutYayamllsSource(t *testing.T) {
	other := "other-linter"
	diag := protocol.Diagnostic{Source: &other}
	if got := actions.Compute("file:///x", "a: b\n", nil, []protocol.Diagnostic{diag}); len(got) != 0 {
		t.Errorf("expected 0 actions for foreign source, got %d", len(got))
	}
}
