package lsp

import (
	"sync"
	"testing"
	"time"

	"github.com/home-operations/yayamlls/internal/render"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// versionRecorder keeps every published version per URI, unlike recorder
// which only retains the last publish.
type versionRecorder struct {
	mu       sync.Mutex
	versions map[string][]uint32
	cleared  map[string]int
}

func (r *versionRecorder) ctx() *glsp.Context {
	r.versions = map[string][]uint32{}
	r.cleared = map[string]int{}
	return &glsp.Context{Notify: func(method string, params any) {
		if method != protocol.ServerTextDocumentPublishDiagnostics {
			return
		}
		p := params.(protocol.PublishDiagnosticsParams)
		r.mu.Lock()
		defer r.mu.Unlock()
		if p.Version == nil {
			r.cleared[p.URI]++
			return
		}
		r.versions[p.URI] = append(r.versions[p.URI], *p.Version)
	}}
}

func (r *versionRecorder) published(uri string) []uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uint32(nil), r.versions[uri]...)
}

func (r *versionRecorder) waitFor(t *testing.T, uri string, v uint32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, got := range r.published(uri) {
			if got >= v {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for diagnostics version %d on %s", v, uri)
}

func wholeChange(uri string, version int32, text string) *protocol.DidChangeTextDocumentParams {
	return &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
			Version:                version,
		},
		ContentChanges: []any{protocol.TextDocumentContentChangeEventWhole{Text: text}},
	}
}

// A burst of didChange notifications must coalesce into a single diagnostics
// pass for the newest version; the intermediate versions are never published.
func TestDidChange_DebounceCoalescesBurst(t *testing.T) {
	rec := &versionRecorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	s.lintDebounce = 20 * time.Millisecond
	uri := "file:///tmp/burst.yaml"

	if err := s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: testLangID, Version: 1, Text: "a: 1\n"},
	}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}
	rec.waitFor(t, uri, 1)

	for i, text := range []string{"a: 2\n", "a: 3\n", "a: 4\n"} {
		v := int32(i + 2)
		if err := s.didChange(ctx, wholeChange(uri, v, text)); err != nil {
			t.Fatalf("didChange v%d: %v", v, err)
		}
	}
	rec.waitFor(t, uri, 4)

	for _, v := range rec.published(uri) {
		if v == 2 || v == 3 {
			t.Fatalf("intermediate version %d was published; want only 1 and 4 (got %v)", v, rec.published(uri))
		}
	}
}

// Closing a document inside the debounce window must drop the pending lint:
// the close clears diagnostics, and nothing resurrects them afterwards.
func TestDidClose_CancelsPendingDebouncedLint(t *testing.T) {
	rec := &versionRecorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	s.lintDebounce = 20 * time.Millisecond
	uri := "file:///tmp/closed.yaml"

	if err := s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: testLangID, Version: 1, Text: "a: 1\n"},
	}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}
	rec.waitFor(t, uri, 1)

	if err := s.didChange(ctx, wholeChange(uri, 2, "a: 2\n")); err != nil {
		t.Fatalf("didChange: %v", err)
	}
	if err := s.didClose(ctx, &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}); err != nil {
		t.Fatalf("didClose: %v", err)
	}

	time.Sleep(5 * s.lintDebounce)
	for _, v := range rec.published(uri) {
		if v >= 2 {
			t.Fatalf("diagnostics for closed document resurrected at version %d", v)
		}
	}
	rec.mu.Lock()
	cleared := rec.cleared[uri]
	rec.mu.Unlock()
	if cleared == 0 {
		t.Fatal("expected an empty-diagnostics publish on close")
	}
}
