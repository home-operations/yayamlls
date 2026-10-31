package lsp

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/home-operations/yayamlls/internal/render"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestExitCodeAndPostShutdownRequests(t *testing.T) {
	handle := func(s *Server, method protocol.Method, params string) error {
		_, _, _, err := s.Handler().Handle(&glsp.Context{Method: method, Params: []byte(params)})
		return err
	}
	fold := `{"textDocument":{"uri":"file:///x.yaml"}}`

	tests := []struct {
		name     string
		shutdown bool
		wantExit int
		wantErr  bool
	}{
		{name: "exit without shutdown", wantExit: 1},
		{name: "exit after shutdown", shutdown: true, wantExit: 0, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New("test", render.NewRegistry())
			if err := handle(s, protocol.MethodInitialize, `{"capabilities":{}}`); err != nil {
				t.Fatalf("initialize: %v", err)
			}
			if tc.shutdown {
				if err := handle(s, protocol.MethodShutdown, `null`); err != nil {
					t.Fatalf("shutdown: %v", err)
				}
			}
			if got := s.ExitCode(); got != tc.wantExit {
				t.Errorf("ExitCode = %d, want %d", got, tc.wantExit)
			}
			if err := handle(s, protocol.MethodTextDocumentFoldingRange, fold); (err != nil) != tc.wantErr {
				t.Errorf("request error = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestInitialize_WorkspaceFoldersCapabilityGatedOnClient(t *testing.T) {
	tests := []struct {
		name string
		caps string
		want bool
	}{
		{"no workspace caps", `{}`, false},
		{"unset", `{"workspace":{}}`, false},
		{"false", `{"workspace":{"workspaceFolders":false}}`, false},
		{"true", `{"workspace":{"workspaceFolders":true}}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var params protocol.InitializeParams
			if err := json.Unmarshal([]byte(`{"capabilities":`+tc.caps+`}`), &params); err != nil {
				t.Fatal(err)
			}
			s := New("test", render.NewRegistry())
			res, err := s.initialize(&glsp.Context{}, &params)
			if err != nil {
				t.Fatal(err)
			}
			caps := res.(protocol.InitializeResult).Capabilities
			got := caps.Workspace != nil && caps.Workspace.WorkspaceFolders != nil
			if got != tc.want {
				t.Errorf("workspaceFolders advertised = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDidClose_DropsPerURIState(t *testing.T) {
	rec := &versionRecorder{}
	ctx := rec.ctx()
	s := New("test", render.NewRegistry())
	s.captureNotify(ctx)
	uri := "file:///tmp/leak.yaml"

	if err := s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: testLangID, Version: 1, Text: "a: 1\n"},
	}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, uri, 1)
	stale, _ := s.docs.Get(uri)
	if err := s.didClose(ctx, &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}); err != nil {
		t.Fatal(err)
	}

	// Late producers for the closed document must leave nothing behind.
	s.Notify(uri, &render.RenderedOutput{Raw: []byte("v")}, nil)
	s.schedulePublish(stale)
	s.setPendingShow(uri, renderKindRendered)
	time.Sleep(50 * time.Millisecond)

	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	s.rendMu.Lock()
	defer s.rendMu.Unlock()
	s.showMu.Lock()
	defer s.showMu.Unlock()
	if len(s.pubSeq) != 0 || len(s.renderedDiags) != 0 || len(s.renderedRaw) != 0 ||
		len(s.renderedBaseline) != 0 || len(s.pendingShow) != 0 {
		t.Fatalf("leaked state: pubSeq=%d diags=%d raw=%d baseline=%d pendingShow=%d",
			len(s.pubSeq), len(s.renderedDiags), len(s.renderedRaw), len(s.renderedBaseline), len(s.pendingShow))
	}
	if got := rec.published(uri); len(got) != 1 {
		t.Errorf("published versions = %v, want only the pre-close publish", got)
	}
}

func TestSchedulePublish_NewestWins(t *testing.T) {
	s := New("test", render.NewRegistry())
	uri := "file:///tmp/order.yaml"
	rec := &versionRecorder{}
	s.captureNotify(rec.ctx())
	d1 := s.docs.Open(uri, testLangID, 1, "a: 1\n")

	s.schedulePublish(d1)
	d2, err := s.docs.Apply(uri, 2, []any{protocol.TextDocumentContentChangeEventWhole{Text: "a: 2\n"}})
	if err != nil {
		t.Fatal(err)
	}
	s.schedulePublish(d2)
	rec.waitFor(t, uri, 2)
	time.Sleep(50 * time.Millisecond)
	got := rec.published(uri)
	if len(got) == 0 || got[len(got)-1] != 2 {
		t.Fatalf("published versions = %v, want last to be 2", got)
	}
}

func TestDebounce_StaleTimerKeepsNewerEntry(t *testing.T) {
	s := New("test", render.NewRegistry())
	s.lintDebounce = 10 * time.Millisecond
	uri := "file:///tmp/deb.yaml"
	other := time.NewTimer(time.Hour)
	defer other.Stop()

	s.debouncePublish(uri)
	s.lintMu.Lock()
	s.lintTimers[uri] = other
	s.lintMu.Unlock()
	time.Sleep(10 * s.lintDebounce)

	s.lintMu.Lock()
	defer s.lintMu.Unlock()
	if s.lintTimers[uri] != other {
		t.Fatal("fired timer removed an entry that is not its own")
	}
}

func TestDidChangeWorkspaceFolders_RootTracking(t *testing.T) {
	const a, b, c = "file:///ws/a", "file:///ws/b", "file:///ws/c"
	folders := func(uris ...string) []protocol.WorkspaceFolder {
		out := make([]protocol.WorkspaceFolder, 0, len(uris))
		for _, u := range uris {
			out = append(out, protocol.WorkspaceFolder{URI: u})
		}
		return out
	}
	tests := []struct {
		name    string
		initial []string
		added   []string
		removed []string
		want    string
	}{
		{"add keeps existing root", []string{a}, []string{b}, nil, a},
		{"add fills empty root", nil, []string{b}, nil, b},
		{"remove active falls back to remaining", []string{a, b}, nil, []string{a}, b},
		{"remove non-active keeps root", []string{a, b}, nil, []string{b}, a},
		{"remove last clears root", []string{a}, nil, []string{a}, ""},
		{"replace active", []string{a}, []string{c}, []string{a}, c},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New("test", render.NewRegistry())
			s.workspaceFolders = tc.initial
			if len(tc.initial) > 0 {
				s.workspaceRoot = tc.initial[0]
			}
			err := s.didChangeWorkspaceFolders(&glsp.Context{}, &protocol.DidChangeWorkspaceFoldersParams{
				Event: protocol.WorkspaceFoldersChangeEvent{Added: folders(tc.added...), Removed: folders(tc.removed...)},
			})
			if err != nil {
				t.Fatal(err)
			}
			if s.workspaceRoot != tc.want {
				t.Errorf("workspaceRoot = %q, want %q", s.workspaceRoot, tc.want)
			}
		})
	}
}

type countingRenderer struct{ renders atomic.Int32 }

func (r *countingRenderer) Name() string                            { return "counting" }
func (r *countingRenderer) Matches(doc *render.SourceDocument) bool { return true }
func (r *countingRenderer) Render(context.Context, *render.SourceDocument) (*render.RenderedOutput, error) {
	r.renders.Add(1)
	return &render.RenderedOutput{Provider: "counting"}, nil
}

func TestApplyLayers_InvalidatesRenderCacheOnlyWhenRendererConfigChanges(t *testing.T) {
	reg := render.NewRegistry()
	cr := &countingRenderer{}
	reg.Register(cr)
	s := New("test", reg)
	uri := "file:///tmp/cache.yaml"
	s.docs.Open(uri, testLangID, 1, "a: 1\n")
	src := &render.SourceDocument{URI: uri, Text: "a: 1\n"}

	apply := func(settings string) {
		t.Helper()
		s.applySettingsRaw(json.RawMessage(settings))
	}
	renderOnce := func(want int32) {
		t.Helper()
		s.pipeline.Schedule(src)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, cached := s.pipeline.Latest(uri, src.Text); cached && cr.renders.Load() >= want {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if got := cr.renders.Load(); got != want {
			t.Fatalf("renders = %d, want %d", got, want)
		}
	}

	apply(`{"renderDebounceMs":1}`)
	renderOnce(1)
	renderOnce(1) // cache hit

	apply(`{"renderDebounceMs":1}`) // identical config: cache survives
	renderOnce(1)

	apply(`{"renderDebounceMs":1,"renderers":{"counting":{"enabled":true}}}`)
	renderOnce(2)
}
