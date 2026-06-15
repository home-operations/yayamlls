package lsp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/home-operations/yayamlls/internal/render"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestReloadWorkspaceConfig_MalformedKeepsPriorSettings(t *testing.T) {
	ws := t.TempDir()
	cfg := filepath.Join(ws, ".yayamlls.yaml")
	if err := os.WriteFile(cfg, []byte("customTags:\n  - \"!Ref\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("test", render.NewRegistry())
	s.settingsMu.Lock()
	s.workspaceRoot = "file://" + ws
	s.settingsMu.Unlock()

	var warnings []string
	ctx := &glsp.Context{Notify: func(method string, params any) {
		if method == protocol.ServerWindowShowMessage {
			warnings = append(warnings, params.(protocol.ShowMessageParams).Message)
		}
	}}

	// First reload: valid config applies the custom tag.
	s.reloadWorkspaceConfig(ctx)
	if got := s.diagnosticOptions().CustomTags; len(got) != 1 || got[0] != "!Ref" {
		t.Fatalf("expected customTags [!Ref] after valid load, got %v", got)
	}

	// Overwrite with a config that fails to parse (catalog must be a bool).
	if err := os.WriteFile(cfg, []byte("catalog: [1, 2, 3]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	warnings = nil
	s.reloadWorkspaceConfig(ctx)

	if got := s.diagnosticOptions().CustomTags; len(got) != 1 || got[0] != "!Ref" {
		t.Fatalf("malformed reload wiped prior settings; customTags = %v, want [!Ref]", got)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning when reload fails to parse")
	}
}
