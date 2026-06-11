package lsp

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/home-operations/yayamlls/internal/render"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestInitialize_CompletionCapabilities(t *testing.T) {
	rec := &recorder{}
	s := New("test", render.NewRegistry())

	res, err := s.initialize(rec.ctx(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	caps := res.(protocol.InitializeResult).Capabilities
	cp := caps.CompletionProvider
	if cp == nil {
		t.Fatal("CompletionProvider not declared")
	}
	for _, want := range []string{":", " ", "-"} {
		if !slices.Contains(cp.TriggerCharacters, want) {
			t.Errorf("trigger characters %v missing %q", cp.TriggerCharacters, want)
		}
	}
	if s.clientSnippets {
		t.Error("clientSnippets true without the capability")
	}
}

func TestInitialize_CapturesSnippetSupport(t *testing.T) {
	rec := &recorder{}
	s := New("test", render.NewRegistry())

	var caps protocol.ClientCapabilities
	raw := `{"textDocument":{"completion":{"completionItem":{"snippetSupport":true}}}}`
	if err := json.Unmarshal([]byte(raw), &caps); err != nil {
		t.Fatal(err)
	}
	if _, err := s.initialize(rec.ctx(), &protocol.InitializeParams{Capabilities: caps}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !s.clientSnippets {
		t.Error("clientSnippets not captured from capabilities")
	}
}
