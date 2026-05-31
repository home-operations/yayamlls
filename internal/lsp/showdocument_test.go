package lsp

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/home-operations/yayamlls/internal/render"
	fileuri "github.com/home-operations/yayamlls/internal/uri"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type callRecorder struct {
	mu   sync.Mutex
	docs []protocol.ShowDocumentParams
}

func (c *callRecorder) ctx() *glsp.Context {
	return &glsp.Context{
		Notify: func(string, any) {},
		Call: func(method string, params any, _ any) {
			if method != protocol.ServerWindowShowDocument {
				return
			}
			c.mu.Lock()
			c.docs = append(c.docs, params.(protocol.ShowDocumentParams))
			c.mu.Unlock()
		},
	}
}

func (c *callRecorder) wait(t *testing.T) protocol.ShowDocumentParams {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		got := append([]protocol.ShowDocumentParams(nil), c.docs...)
		c.mu.Unlock()
		if len(got) > 0 {
			return got[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for window/showDocument")
	return protocol.ShowDocumentParams{}
}

func seedRender(s *Server, uri, raw string) {
	s.Notify(uri, &render.RenderedOutput{Provider: "flate", Raw: []byte(raw)}, nil)
}

func TestShowRendered_OpensReadyRenderViaShowDocument(t *testing.T) {
	rec := &callRecorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	s.clientShowDoc = true
	s.captureNotify(ctx)

	uri := "file:///tmp/hr.yaml"
	seedRender(s, uri, "kind: Pod\n")

	res, err := s.executeCommand(ctx, &protocol.ExecuteCommandParams{
		Command:   CommandShowRendered,
		Arguments: []any{uri},
	})
	if err != nil {
		t.Fatalf("executeCommand: %v", err)
	}
	if res != nil {
		t.Errorf("showDocument client should get a nil result, got %v", res)
	}

	p := rec.wait(t)
	path := fileuri.ToPath(p.URI)
	defer func() { _ = os.Remove(path) }()
	if !strings.HasSuffix(path, ".rendered.yaml") {
		t.Errorf("unexpected temp extension: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	if string(b) != "kind: Pod\n" {
		t.Errorf("temp content = %q, want %q", b, "kind: Pod\n")
	}
}

func TestShowRendered_DefersUntilRenderLands(t *testing.T) {
	rec := &callRecorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	s.clientShowDoc = true
	s.captureNotify(ctx)

	uri := "file:///tmp/hr-deferred.yaml"
	// Render not ready: the command should defer, showing nothing yet.
	if _, err := s.executeCommand(ctx, &protocol.ExecuteCommandParams{
		Command:   CommandShowRendered,
		Arguments: []any{uri},
	}); err != nil {
		t.Fatalf("executeCommand: %v", err)
	}
	if len(rec.docs) != 0 {
		t.Fatalf("expected no showDocument before render lands, got %d", len(rec.docs))
	}

	// The pipeline reports back; the deferred show fires.
	seedRender(s, uri, "kind: Service\n")
	p := rec.wait(t)
	path := fileuri.ToPath(p.URI)
	defer func() { _ = os.Remove(path) }()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	if string(b) != "kind: Service\n" {
		t.Errorf("temp content = %q", b)
	}
}

func TestShowRenderedDiff_UsesDiffExtension(t *testing.T) {
	rec := &callRecorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	s.clientShowDoc = true
	s.captureNotify(ctx)

	uri := "file:///tmp/hr-diff.yaml"
	seedRender(s, uri, "kind: Pod\n")        // becomes the baseline
	seedRender(s, uri, "kind: Deployment\n") // current diverges from baseline

	if _, err := s.executeCommand(ctx, &protocol.ExecuteCommandParams{
		Command:   CommandShowRenderedDiff,
		Arguments: []any{uri},
	}); err != nil {
		t.Fatalf("executeCommand: %v", err)
	}
	p := rec.wait(t)
	path := fileuri.ToPath(p.URI)
	defer func() { _ = os.Remove(path) }()
	if !strings.HasSuffix(path, ".rendered.diff") {
		t.Errorf("unexpected temp extension: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	if !strings.Contains(string(b), "Deployment") {
		t.Errorf("diff missing current content: %s", b)
	}
}

func TestShowRendered_LegacyPayloadWithoutShowDocument(t *testing.T) {
	s := New("test", render.NewRegistry())
	// clientShowDoc stays false: simulate a client lacking window/showDocument.
	uri := "file:///tmp/hr-legacy.yaml"
	seedRender(s, uri, "kind: Pod\n")

	res, err := s.executeCommand(&glsp.Context{Notify: func(string, any) {}}, &protocol.ExecuteCommandParams{
		Command:   CommandShowRendered,
		Arguments: []any{uri},
	})
	if err != nil {
		t.Fatalf("executeCommand: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected a payload map, got %T", res)
	}
	if m[resultKeyYAML] != "kind: Pod\n" {
		t.Errorf("payload[%s] = %v", resultKeyYAML, m[resultKeyYAML])
	}
}
