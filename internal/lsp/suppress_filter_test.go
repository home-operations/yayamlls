package lsp

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/home-operations/yayamlls/internal/actions"
	"github.com/home-operations/yayamlls/internal/render"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func nestedModeline(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return "# yaml-language-server: $schema=" + filepath.Join(repo, "test", "fixtures", "schemas", "nested.json") + "\n"
}

// applyInsert applies a single zero-width insert TextEdit (character precision).
func applyInsert(text string, e protocol.TextEdit) string {
	lines := strings.Split(text, "\n")
	l, c := int(e.Range.Start.Line), int(e.Range.Start.Character)
	lines[l] = lines[l][:c] + e.NewText + lines[l][c:]
	return strings.Join(lines, "\n")
}

// A missing/unknown key anchors its diagnostic on the mapping's content line.
// The suppress quick-fix must actually silence it once applied.
func TestSuppress_StructuralError_Filtered(t *testing.T) {
	rec := &recorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	uri := "file:///tmp/nested.yaml"
	// "hello" satisfies required; "extra" is the lone unknown-key error, which
	// anchors on the "extra" key line — not the mapping's first child.
	text := nestedModeline(t) + "spec:\n  hello: true\n  extra: 1\n"

	if err := s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: testLangID, Version: 1, Text: text},
	}); err != nil {
		t.Fatal(err)
	}
	rec.waitVersion(t, uri, 1)
	diags := rec.diags(uri)
	if len(diags) == 0 {
		t.Fatal("expected structural diagnostics")
	}

	d := diags[0]
	acts := actions.Compute(uri, text, s.schemaAtCursor(uri, d.Range.Start), []protocol.Diagnostic{d})
	var edit protocol.TextEdit
	found := false
	for _, a := range acts {
		if a.Title == "Suppress this diagnostic" {
			edit = a.Edit.Changes[uri][0]
			found = true
		}
	}
	if !found {
		t.Fatal("no suppress action offered")
	}
	newText := applyInsert(text, edit)

	if err := s.didChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri}, Version: 2},
		ContentChanges: []any{protocol.TextDocumentContentChangeEventWhole{Text: newText}},
	}); err != nil {
		t.Fatal(err)
	}
	rec.waitVersion(t, uri, 2)
	if got := rec.diags(uri); len(got) != 0 {
		t.Fatalf("structural diagnostics not suppressed after applying the fix: %d remain:\n%s", len(got), newText)
	}
}
