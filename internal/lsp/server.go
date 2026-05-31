package lsp

import (
	"encoding/json"
	"sync"

	"github.com/home-operations/yayamlls/internal/config"
	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/document"
	"github.com/home-operations/yayamlls/internal/lint"
	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const lsName = "yayamlls"

type Server struct {
	docs     *document.Store
	schemas  *schema.Store
	resolver *schema.Resolver
	renderer *render.Registry
	pipeline *render.Pipeline
	handler  *protocol.Handler
	version  string

	rendMu           sync.Mutex
	renderedDiags    map[string][]protocol.Diagnostic
	renderedRaw      map[string][]byte
	renderedBaseline map[string][]byte

	// connNotify and connCall are captured lazily from the first incoming
	// request so the async render pipeline can push diagnostics, and fire
	// window/showDocument, without holding onto a per-request *glsp.Context.
	connMu     sync.Mutex
	connNotify glsp.NotifyFunc
	connCall   glsp.CallFunc

	// clientShowDoc records whether the client advertised window/showDocument
	// at initialize. When set, the showRendered commands open the result in the
	// editor; otherwise they return it as a payload for the bundled extensions.
	clientShowDoc bool

	// pendingShow holds show commands that arrived before the render was ready,
	// keyed by URI; Notify fires the deferred window/showDocument once it lands.
	showMu      sync.Mutex
	pendingShow map[string]string

	// pubSeq supersedes async diagnostic publishes: each publish bumps the
	// per-URI counter, and a goroutine drops its result if a newer one started.
	pubMu  sync.Mutex
	pubSeq map[string]uint64

	// settings is the effective merge; workspaceSettings (from .yayamlls.yaml)
	// is the lowest-precedence layer and overrides holds initializationOptions
	// + didChangeConfiguration. Tracking the layers separately lets a
	// workspace-folder change reload the file layer without discarding the
	// higher-precedence client config.
	settingsMu        sync.Mutex
	settings          config.Settings
	workspaceSettings config.Settings
	overrides         config.Settings
	workspaceRoot     string
}

func New(version string, registry *render.Registry) *Server {
	if registry == nil {
		registry = render.NewRegistry()
	}
	s := &Server{
		docs:             document.NewStore(),
		schemas:          schema.NewStore(),
		resolver:         schema.NewResolver(),
		renderer:         registry,
		renderedDiags:    make(map[string][]protocol.Diagnostic),
		renderedRaw:      make(map[string][]byte),
		renderedBaseline: make(map[string][]byte),
		pubSeq:           make(map[string]uint64),
		pendingShow:      make(map[string]string),
		version:          version,
	}
	s.pipeline = render.NewPipeline(registry, s)
	s.resolver.SetReloadHook(s.republishOpen)
	s.handler = &protocol.Handler{
		Initialize:    s.initialize,
		Initialized:   s.initialized,
		Shutdown:      s.shutdown,
		SetTrace:      s.setTrace,
		CancelRequest: s.cancelRequest,

		TextDocumentDidOpen:   s.didOpen,
		TextDocumentDidChange: s.didChange,
		TextDocumentDidClose:  s.didClose,

		TextDocumentHover:          s.hover,
		TextDocumentCompletion:     s.completion,
		TextDocumentFoldingRange:   s.foldingRange,
		TextDocumentDocumentLink:   s.documentLink,
		TextDocumentDocumentSymbol: s.documentSymbol,
		TextDocumentCodeAction:     s.codeAction,
		TextDocumentCodeLens:       s.codeLens,

		WorkspaceDidChangeConfiguration:    s.didChangeConfig,
		WorkspaceDidChangeWorkspaceFolders: s.didChangeWorkspaceFolders,
		WorkspaceExecuteCommand:            s.executeCommand,
	}
	return s
}

func (s *Server) Handler() *protocol.Handler { return s.handler }

func (s *Server) initialize(ctx *glsp.Context, params *protocol.InitializeParams) (any, error) {
	caps := s.handler.CreateServerCapabilities()
	change := protocol.TextDocumentSyncKindIncremental
	caps.TextDocumentSync = &protocol.TextDocumentSyncOptions{
		OpenClose: ptr(true),
		Change:    &change,
	}
	caps.ExecuteCommandProvider = &protocol.ExecuteCommandOptions{
		Commands: []string{CommandShowRendered, CommandShowRenderedDiff},
	}
	caps.CodeActionProvider = &protocol.CodeActionOptions{
		CodeActionKinds: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix},
	}

	if w := params.Capabilities.Window; w != nil && w.ShowDocument != nil && w.ShowDocument.Support {
		s.clientShowDoc = true
	}

	root := pickWorkspaceRoot(params)
	var (
		ws  config.Settings
		err error
	)
	if root != "" {
		s.workspaceRoot = root
		ws, err = config.LoadFromWorkspace(root)
		if err != nil {
			notifyShowMessage(ctx, protocol.MessageTypeWarning,
				"yayamlls: failed to load .yayamlls.yaml: "+err.Error())
		}
	}
	s.settingsMu.Lock()
	s.workspaceSettings = ws
	if init := settingsFromInitOptions(params.InitializationOptions); init != nil {
		s.overrides = *init
	}
	s.settingsMu.Unlock()
	s.applyLayers()

	return protocol.InitializeResult{
		Capabilities: caps,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    lsName,
			Version: &s.version,
		},
	}, nil
}

func pickWorkspaceRoot(params *protocol.InitializeParams) string {
	if len(params.WorkspaceFolders) > 0 {
		return params.WorkspaceFolders[0].URI
	}
	if params.RootURI != nil {
		return *params.RootURI
	}
	return ""
}

func settingsFromInitOptions(opts any) *config.Settings {
	if opts == nil {
		return nil
	}
	var raw json.RawMessage
	switch v := opts.(type) {
	case json.RawMessage:
		raw = v
	default:
		b, err := json.Marshal(opts)
		if err != nil {
			return nil
		}
		raw = b
	}
	parsed, err := config.Parse(raw)
	if err != nil {
		return nil
	}
	return &parsed
}

// applyLayers recomputes the effective settings from the workspace layer
// (lowest precedence) and the overrides layer, then pushes them to the
// resolver and renderers.
func (s *Server) applyLayers() {
	s.settingsMu.Lock()
	effective := config.Merge(s.workspaceSettings, s.overrides)
	s.settings = effective
	s.settingsMu.Unlock()
	s.resolver.SetSettings(effective)
	s.renderer.SetWorkspaceRoot(uriToPath(s.workspaceRoot))
	s.renderer.Configure(effective.Renderers)
}

// setWorkspaceLayer replaces the .yayamlls.yaml layer (e.g. after a
// workspace-folder change) while preserving the override layer.
func (s *Server) setWorkspaceLayer(ws config.Settings) {
	s.settingsMu.Lock()
	s.workspaceSettings = ws
	s.settingsMu.Unlock()
	s.applyLayers()
}

func (s *Server) applySettingsRaw(raw json.RawMessage) {
	settings, err := config.Parse(raw)
	if err != nil {
		return
	}
	s.settingsMu.Lock()
	s.overrides = config.Merge(s.overrides, settings)
	s.settingsMu.Unlock()
	s.applyLayers()
}

func notifyShowMessage(ctx *glsp.Context, level protocol.MessageType, msg string) {
	if ctx == nil || ctx.Notify == nil {
		return
	}
	ctx.Notify(protocol.ServerWindowShowMessage, protocol.ShowMessageParams{
		Type:    level,
		Message: msg,
	})
}

func (s *Server) initialized(ctx *glsp.Context, params *protocol.InitializedParams) error {
	return nil
}

func (s *Server) shutdown(ctx *glsp.Context) error {
	protocol.SetTraceValue(protocol.TraceValueOff)
	return nil
}

func (s *Server) setTrace(ctx *glsp.Context, params *protocol.SetTraceParams) error {
	protocol.SetTraceValue(params.Value)
	return nil
}

func (s *Server) cancelRequest(ctx *glsp.Context, params *protocol.CancelParams) error {
	return nil
}

func ptr[T any](v T) *T { return &v }

func (s *Server) Notify(uri string, out *render.RenderedOutput, err error) {
	diags := renderDiagnostics(s.schemas, s.resolver, out, err)

	s.rendMu.Lock()
	s.renderedDiags[uri] = diags
	if out != nil {
		s.renderedRaw[uri] = out.Raw
		if _, ok := s.renderedBaseline[uri]; !ok {
			s.renderedBaseline[uri] = append([]byte(nil), out.Raw...)
		}
	}
	s.rendMu.Unlock()

	if kind, ok := s.takePendingShow(uri); ok {
		go s.showInEditor(s.currentCall(), uri, kind)
	}

	d, ok := s.docs.Get(uri)
	if !ok {
		return
	}
	s.schedulePublish(d)
}

// republishOpen recomputes diagnostics for every open document. Used as the
// resolver's reload hook so docs resolved via the background-loaded catalog
// get their diagnostics once it becomes available.
func (s *Server) republishOpen() {
	for _, uri := range s.docs.AllURIs() {
		if d, ok := s.docs.Get(uri); ok {
			s.schedulePublish(d)
		}
	}
}

func (s *Server) captureNotify(ctx *glsp.Context) {
	if ctx == nil {
		return
	}
	s.connMu.Lock()
	if s.connNotify == nil && ctx.Notify != nil {
		s.connNotify = ctx.Notify
	}
	if s.connCall == nil && ctx.Call != nil {
		s.connCall = ctx.Call
	}
	s.connMu.Unlock()
}

// schedulePublish computes and publishes diagnostics off the message loop.
// lint.Document can block on a network schema fetch and glsp dispatches
// serially, so running it inline would freeze every other file until the fetch
// returns. The sequence guard drops a result superseded by a newer edit or a
// close.
func (s *Server) schedulePublish(d *document.Document) {
	notify := s.currentNotify()
	if notify == nil {
		return
	}
	uri := d.URI
	s.pubMu.Lock()
	s.pubSeq[uri]++
	seq := s.pubSeq[uri]
	s.pubMu.Unlock()

	go func() {
		// nil marshals to `null`; clients keep stale diagnostics on `null`.
		diags := lint.Document(d.Text, uriToPath(uri), s.resolver, s.schemas)
		diags = append(diags, s.renderedDiagnosticsFor(uri)...)
		diags = diagnostics.ParseSuppressions(d.Text).Filter(diags)

		s.pubMu.Lock()
		current := s.pubSeq[uri] == seq
		s.pubMu.Unlock()
		if !current {
			return
		}
		v := protocol.UInteger(d.Version)
		notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
			URI:         uri,
			Version:     &v,
			Diagnostics: diags,
		})
	}()
}

func (s *Server) currentNotify() glsp.NotifyFunc {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.connNotify
}

func (s *Server) currentCall() glsp.CallFunc {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.connCall
}

// forgetPublish drops a closed document's counter so an in-flight publish
// goroutine sees it was superseded and won't resurrect diagnostics.
func (s *Server) forgetPublish(uri string) {
	s.pubMu.Lock()
	delete(s.pubSeq, uri)
	s.pubMu.Unlock()
}

func (s *Server) renderedDiagnosticsFor(uri string) []protocol.Diagnostic {
	s.rendMu.Lock()
	defer s.rendMu.Unlock()
	if d, ok := s.renderedDiags[uri]; ok {
		out := make([]protocol.Diagnostic, len(d))
		copy(out, d)
		return out
	}
	return nil
}

func (s *Server) renderedRawFor(uri string) []byte {
	s.rendMu.Lock()
	defer s.rendMu.Unlock()
	return s.renderedRaw[uri]
}

func (s *Server) renderedBaselineFor(uri string) []byte {
	s.rendMu.Lock()
	defer s.rendMu.Unlock()
	return s.renderedBaseline[uri]
}

func (s *Server) clearRenderState(uri string) {
	s.rendMu.Lock()
	delete(s.renderedDiags, uri)
	delete(s.renderedRaw, uri)
	delete(s.renderedBaseline, uri)
	s.rendMu.Unlock()
}
