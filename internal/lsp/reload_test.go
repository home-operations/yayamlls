package lsp

import (
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/home-operations/yamlls/internal/render"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const testLangID = "yaml"

func schemaModeline(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	schema := filepath.Join(repo, "test", "fixtures", "schemas", "person.json")
	return "# yaml-language-server: $schema=" + schema + "\n"
}

type recorder struct {
	mu   sync.Mutex
	last map[string][]protocol.Diagnostic
}

func (r *recorder) ctx() *glsp.Context {
	r.last = map[string][]protocol.Diagnostic{}
	return &glsp.Context{Notify: func(method string, params any) {
		if method != protocol.ServerTextDocumentPublishDiagnostics {
			return
		}
		p := params.(protocol.PublishDiagnosticsParams)
		r.mu.Lock()
		r.last[p.URI] = p.Diagnostics
		r.mu.Unlock()
	}}
}

func (r *recorder) diags(uri string) []protocol.Diagnostic {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last[uri]
}

func TestDiagnostics_ReloadOnWholeChange(t *testing.T) {
	ml := schemaModeline(t)
	rec := &recorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	uri := "file:///tmp/person.yaml"

	if err := s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: testLangID, Version: 1, Text: ml + "name: Alice\nage: \"thirty\"\n"},
	}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}
	if len(rec.diags(uri)) == 0 {
		t.Fatalf("expected a diagnostic for the bad age, got none")
	}

	if err := s.didChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri}, Version: 2},
		ContentChanges: []any{protocol.TextDocumentContentChangeEventWhole{Text: ml + "name: Alice\nage: 30\n"}},
	}); err != nil {
		t.Fatalf("didChange: %v", err)
	}
	if got := rec.diags(uri); len(got) != 0 {
		t.Fatalf("expected diagnostics cleared after fix, got %+v", got)
	}
}

func TestDiagnostics_ReloadOnIncrementalChange(t *testing.T) {
	ml := schemaModeline(t)
	rec := &recorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	uri := "file:///tmp/person.yaml"

	// age is valid on open.
	open := ml + "name: Alice\nage: 30\n"
	if err := s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: testLangID, Version: 1, Text: open},
	}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}
	if got := rec.diags(uri); len(got) != 0 {
		t.Fatalf("expected no diagnostics on open, got %+v", got)
	}

	// Incrementally replace `30` with `"x"` on the age line (line index 2).
	ageLine := uint32(2)
	if err := s.didChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri}, Version: 2},
		ContentChanges: []any{protocol.TextDocumentContentChangeEvent{
			Range: &protocol.Range{
				Start: protocol.Position{Line: ageLine, Character: 5},
				End:   protocol.Position{Line: ageLine, Character: 7},
			},
			Text: "\"x\"",
		}},
	}); err != nil {
		t.Fatalf("didChange: %v", err)
	}
	if got := rec.diags(uri); len(got) == 0 {
		t.Fatalf("expected a diagnostic after making age a string, got none")
	}
}
