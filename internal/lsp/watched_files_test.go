package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/home-operations/yayamlls/internal/render"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// registrationRecorder captures client/registerCapability requests.
type registrationRecorder struct {
	mu     sync.Mutex
	params []protocol.RegistrationParams
}

func (r *registrationRecorder) ctx() *glsp.Context {
	return &glsp.Context{
		Notify: func(string, any) {},
		Call: func(method string, params any, _ any) {
			if method != protocol.ServerClientRegisterCapability {
				return
			}
			r.mu.Lock()
			r.params = append(r.params, params.(protocol.RegistrationParams))
			r.mu.Unlock()
		},
	}
}

func (r *registrationRecorder) recorded() []protocol.RegistrationParams {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]protocol.RegistrationParams(nil), r.params...)
}

// watchCapabilities builds ClientCapabilities advertising dynamic
// registration for didChangeWatchedFiles. Built via JSON because glsp nests
// the field in anonymous structs that can't be named in a literal.
func watchCapabilities(t *testing.T) protocol.ClientCapabilities {
	t.Helper()
	var caps protocol.ClientCapabilities
	raw := `{"workspace":{"didChangeWatchedFiles":{"dynamicRegistration":true}}}`
	if err := json.Unmarshal([]byte(raw), &caps); err != nil {
		t.Fatal(err)
	}
	return caps
}

func TestInitialized_RegistersFileWatchers(t *testing.T) {
	rec := &registrationRecorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())

	if _, err := s.initialize(ctx, &protocol.InitializeParams{Capabilities: watchCapabilities(t)}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !s.clientWatchFiles {
		t.Fatal("clientWatchFiles not captured from capabilities")
	}
	if err := s.initialized(ctx, &protocol.InitializedParams{}); err != nil {
		t.Fatalf("initialized: %v", err)
	}

	// Registration is issued asynchronously; poll for it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := rec.recorded(); len(got) > 0 {
			regs := got[0].Registrations
			if len(regs) != 1 {
				t.Fatalf("expected 1 registration, got %d", len(regs))
			}
			if regs[0].Method != protocol.MethodWorkspaceDidChangeWatchedFiles {
				t.Fatalf("registration method = %q", regs[0].Method)
			}
			opts := regs[0].RegisterOptions.(protocol.DidChangeWatchedFilesRegistrationOptions)
			if len(opts.Watchers) < 2 {
				t.Fatalf("expected at least 2 watchers, got %+v", opts.Watchers)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("registration call never issued")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestInitialized_NoRegistrationWithoutCapability(t *testing.T) {
	rec := &registrationRecorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())

	if _, err := s.initialize(ctx, &protocol.InitializeParams{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := s.initialized(ctx, &protocol.InitializedParams{}); err != nil {
		t.Fatalf("initialized: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if got := rec.recorded(); len(got) != 0 {
		t.Fatalf("unexpected registration without dynamicRegistration: %+v", got)
	}
}

func TestDidChangeWatchedFiles_ReloadsWorkspaceConfig(t *testing.T) {
	root := t.TempDir()
	rec := &recorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())

	rootURI := "file://" + root
	if _, err := s.initialize(ctx, &protocol.InitializeParams{RootURI: &rootURI}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !s.kubernetesEnabled() {
		t.Fatal("kubernetes should default to enabled")
	}

	cfg := filepath.Join(root, ".yayamlls.yaml")
	if err := os.WriteFile(cfg, []byte("kubernetes:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.didChangeWatchedFiles(ctx, &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: "file://" + cfg, Type: protocol.FileChangeTypeCreated}},
	}); err != nil {
		t.Fatalf("didChangeWatchedFiles: %v", err)
	}
	if s.kubernetesEnabled() {
		t.Fatal("config change not applied")
	}
}

// fakeTreeRenderer records InvalidateTree calls.
type fakeTreeRenderer struct {
	mu          sync.Mutex
	invalidated []string
}

func (f *fakeTreeRenderer) Name() string                            { return "faketree" }
func (f *fakeTreeRenderer) Matches(doc *render.SourceDocument) bool { return false }
func (f *fakeTreeRenderer) Render(ctx context.Context, doc *render.SourceDocument) (*render.RenderedOutput, error) {
	return &render.RenderedOutput{Provider: f.Name()}, nil
}

func (f *fakeTreeRenderer) InvalidateTree(path string) {
	f.mu.Lock()
	f.invalidated = append(f.invalidated, path)
	f.mu.Unlock()
}

func TestDidChangeWatchedFiles_InvalidatesRendererTrees(t *testing.T) {
	reg := render.NewRegistry()
	fr := &fakeTreeRenderer{}
	reg.Register(fr)
	rec := &recorder{}
	ctx := rec.ctx()
	s := New("test", reg)

	if err := s.didChangeWatchedFiles(ctx, &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: "file:///ws/apps/cm.yaml", Type: protocol.FileChangeTypeChanged}},
	}); err != nil {
		t.Fatalf("didChangeWatchedFiles: %v", err)
	}
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if len(fr.invalidated) != 1 || fr.invalidated[0] != "/ws/apps/cm.yaml" {
		t.Fatalf("InvalidateTree calls = %+v", fr.invalidated)
	}
}

func TestInitialized_RegistersWatchersForNonYAMLRenderInputs(t *testing.T) {
	rec := &registrationRecorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	if _, err := s.initialize(ctx, &protocol.InitializeParams{Capabilities: watchCapabilities(t)}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := s.initialized(ctx, &protocol.InitializedParams{}); err != nil {
		t.Fatalf("initialized: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := rec.recorded()
		if len(got) > 0 {
			opts := got[0].Registrations[0].RegisterOptions.(protocol.DidChangeWatchedFilesRegistrationOptions)
			var hasNonYAML bool
			for _, w := range opts.Watchers {
				if !strings.Contains(w.GlobPattern, "yaml") &&
					!strings.Contains(w.GlobPattern, "yml") &&
					!strings.Contains(w.GlobPattern, "yayamlls") {
					hasNonYAML = true
					break
				}
			}
			if !hasNonYAML {
				t.Fatalf("no watcher covers non-YAML render inputs (chart .tpl etc.); watchers = %+v", opts.Watchers)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("registration call never issued")
}
