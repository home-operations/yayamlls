package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

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

// defaultLintDebounce coalesces a burst of didChange notifications into one
// diagnostics pass. It is intentionally shorter than the render debounce so
// schema feedback still feels immediate.
const defaultLintDebounce = 200 * time.Millisecond

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

	// clientWatchFiles records whether the client supports dynamic
	// registration for workspace/didChangeWatchedFiles (the spec offers no
	// static form). When set, initialized registers watchers and renderers
	// switch to event-driven tree invalidation.
	clientWatchFiles bool

	// clientSnippets records whether the client supports snippet completion
	// items (tab stops and placeholders in insert text).
	clientSnippets bool

	// pendingShow holds show commands that arrived before the render was ready,
	// keyed by URI; Notify fires the deferred window/showDocument once it lands.
	showMu      sync.Mutex
	pendingShow map[string]string

	// pubSeq supersedes async diagnostic publishes: each publish bumps the
	// per-URI counter, and a goroutine drops its result if a newer one started.
	pubMu  sync.Mutex
	pubSeq map[string]uint64

	// lintTimers debounces didChange diagnostics per URI so typing doesn't
	// trigger a parse+validate per keystroke. didOpen and config changes
	// publish immediately. lintDebounce is set once in New (tests lower it).
	lintMu       sync.Mutex
	lintTimers   map[string]*time.Timer
	lintDebounce time.Duration

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
		lintTimers:       make(map[string]*time.Timer),
		lintDebounce:     defaultLintDebounce,
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
		WorkspaceDidChangeWatchedFiles:     s.didChangeWatchedFiles,
		WorkspaceExecuteCommand:            s.executeCommand,
	}
	return s
}

func (s *Server) Handler() *protocol.Handler { return s.handler }

// kubernetesEnabled gates the code-lens and render paths, which don't go
// through the resolver.
func (s *Server) kubernetesEnabled() bool {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.settings.KubernetesEnabled()
}

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
	// ":" fires value completion as soon as a key's colon is typed, " " in
	// value/sequence position, "-" when starting a sequence item.
	caps.CompletionProvider = &protocol.CompletionOptions{
		TriggerCharacters: []string{":", " ", "-"},
	}
	// glsp wires the WorkspaceDidChangeWorkspaceFolders handler but doesn't set
	// this capability, and clients withhold the notification until it's declared.
	caps.Workspace = &protocol.ServerCapabilitiesWorkspace{
		WorkspaceFolders: &protocol.WorkspaceFoldersServerCapabilities{
			Supported:           ptr(true),
			ChangeNotifications: &protocol.BoolOrString{Value: true},
		},
	}

	if w := params.Capabilities.Window; w != nil && w.ShowDocument != nil && w.ShowDocument.Support {
		s.clientShowDoc = true
	}
	if td := params.Capabilities.TextDocument; td != nil && td.Completion != nil &&
		td.Completion.CompletionItem != nil && td.Completion.CompletionItem.SnippetSupport != nil {
		s.clientSnippets = *td.Completion.CompletionItem.SnippetSupport
	}
	if w := params.Capabilities.Workspace; w != nil && w.DidChangeWatchedFiles != nil &&
		w.DidChangeWatchedFiles.DynamicRegistration != nil && *w.DidChangeWatchedFiles.DynamicRegistration {
		s.clientWatchFiles = true
	}

	root := pickWorkspaceRoot(params)
	var (
		ws  config.Settings
		err error
	)
	if root != "" {
		ws, err = config.LoadFromWorkspace(root)
		if err != nil {
			notifyShowMessage(ctx, protocol.MessageTypeWarning,
				"yayamlls: failed to load .yayamlls.yaml: "+err.Error())
		}
	}
	s.settingsMu.Lock()
	if root != "" {
		s.workspaceRoot = root
	}
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
	ws := s.workspaceSettings
	ov := s.overrides
	effective := config.Merge(ws, ov)
	s.settings = effective
	root := s.workspaceRoot
	s.settingsMu.Unlock()
	s.resolver.SetSettings(effective)
	s.schemas.SetTrustRoot(uriToPath(root))
	s.renderer.SetWorkspaceRoot(uriToPath(root))
	// Subprocess renderer commands from an untrusted workspace .yayamlls.yaml
	// are dropped here (config.TrustedRenderers); only command-less entries and
	// the trusted client layer reach the registry.
	renderers, dropped := config.TrustedRenderers(ws, ov)
	if len(dropped) > 0 {
		s.warn(fmt.Sprintf("yayamlls: ignored workspace renderer command(s) %s from .yayamlls.yaml for safety; "+
			"declare subprocess renderers in your editor/global config instead", strings.Join(dropped, ", ")))
	}
	if errs := s.renderer.Configure(renderers); len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		s.warn("yayamlls: renderer config rejected: " + strings.Join(msgs, "; "))
	}
	debounce := render.DefaultDebounce
	if ms := effective.RenderDebounceMs; ms != nil && *ms > 0 {
		debounce = time.Duration(*ms) * time.Millisecond
	}
	s.pipeline.SetDebounce(debounce)
	timeout := render.DefaultTimeout
	if ms := effective.RenderTimeoutMs; ms != nil && *ms > 0 {
		timeout = time.Duration(*ms) * time.Millisecond
	}
	s.pipeline.SetTimeout(timeout)
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

// warn sends a best-effort window/showMessage warning over the captured
// connection. It is a no-op before the connection's notify func has been
// captured (e.g. during initialize), so callers use it for non-fatal config
// problems that also surface on a later reload.
func (s *Server) warn(msg string) {
	if n := s.currentNotify(); n != nil {
		n(protocol.ServerWindowShowMessage, protocol.ShowMessageParams{
			Type:    protocol.MessageTypeWarning,
			Message: msg,
		})
	}
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
	s.captureNotify(ctx)
	if !s.clientWatchFiles {
		return nil
	}
	// Optimistic: a client advertising dynamicRegistration honors the
	// registration. Non-watching clients keep the fingerprint fallback.
	s.renderer.SetFileWatchActive(true)
	call := s.currentCall()
	if call == nil {
		return nil
	}
	// glsp dispatches on the jsonrpc2 read loop, so a blocking Call from
	// inside a handler would deadlock waiting for its own response; register
	// asynchronously (same pattern as showInEditor).
	go func() {
		var result any
		call(protocol.ServerClientRegisterCapability, protocol.RegistrationParams{
			Registrations: []protocol.Registration{{
				ID:     "yayamlls.watchedFiles",
				Method: protocol.MethodWorkspaceDidChangeWatchedFiles,
				RegisterOptions: protocol.DidChangeWatchedFilesRegistrationOptions{
					Watchers: []protocol.FileSystemWatcher{
						{GlobPattern: "**/*.{yaml,yml}"},
						// Catch non-YAML inputs that renderers depend on:
						// in-repo Helm chart .tpl files, kustomize
						// configMapGenerator inputs, etc. Without this, a
						// file-system change outside the YAML globs is
						// invisible to the event-driven invalidation path
						// and the renderer replays stale output.
						{GlobPattern: "**/*.{tpl,conf,json,toml,env}"},
						// Separate watcher: `*` does not match a leading dot
						// in every client's glob implementation.
						{GlobPattern: "**/.{yayamlls,yamlls}.yaml"},
					},
				},
			}},
		}, &result)
	}()
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

// cancelRequest is deliberately a no-op. glsp dispatches every message
// synchronously on the jsonrpc2 read goroutine and exposes no request ID to
// handlers, so by the time a $/cancelRequest is read off the wire the request
// it targets has always completed. The useful equivalents exist elsewhere:
// the render pipeline supersedes-and-cancels on newer content, pubSeq drops
// superseded diagnostics, and renders carry a timeout.
func (s *Server) cancelRequest(ctx *glsp.Context, params *protocol.CancelParams) error {
	return nil
}

func ptr[T any](v T) *T { return &v }

// diagnosticOptions derives the validation options from the current
// effective settings.
func (s *Server) diagnosticOptions() diagnostics.Options {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return diagnostics.Options{FluxSubstitutions: s.settings.FluxSubstitutionsEnabled(), CustomTags: s.settings.CustomTagNames()}
}

func (s *Server) Notify(uri string, out *render.RenderedOutput, err error) {
	diags := renderDiagnostics(s.schemas, s.resolver, out, err, s.diagnosticOptions())

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

	opts := s.diagnosticOptions()

	go func() {
		// nil marshals to `null`; clients keep stale diagnostics on `null`.
		diags := lint.Document(d.Parsed(), uriToPath(uri), s.resolver, s.schemas, opts)
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

// debouncePublish coalesces a burst of didChange notifications into one
// diagnostics pass. The timer re-reads the store when it fires, so it always
// lints the newest text, and a close in the window finds no document and
// publishes nothing.
func (s *Server) debouncePublish(uri string) {
	s.lintMu.Lock()
	if t, ok := s.lintTimers[uri]; ok {
		t.Stop()
	}
	s.lintTimers[uri] = time.AfterFunc(s.lintDebounce, func() {
		s.lintMu.Lock()
		delete(s.lintTimers, uri)
		s.lintMu.Unlock()
		if d, ok := s.docs.Get(uri); ok {
			s.publishDiagnostics(nil, d)
		}
	})
	s.lintMu.Unlock()
}

// cancelDebounce drops a pending debounced publish, called from didClose so
// a timer can't fire for a document that is no longer open.
func (s *Server) cancelDebounce(uri string) {
	s.lintMu.Lock()
	if t, ok := s.lintTimers[uri]; ok {
		t.Stop()
		delete(s.lintTimers, uri)
	}
	s.lintMu.Unlock()
}

// forgetPublish advances a closed document's counter so an in-flight publish
// goroutine that captured the prior seq can never match again. Deleting the
// entry would let a close+reopen of the same URI restart the sequence and
// resurrect diagnostics for the pre-close text.
func (s *Server) forgetPublish(uri string) {
	s.pubMu.Lock()
	s.pubSeq[uri]++
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

// renderedRawAndBaseline returns raw and baseline for uri under a single
// rendMu acquisition, so a concurrent Notify can't write between the two
// reads and produce a diff against a one-generation-stale raw.
func (s *Server) renderedRawAndBaseline(uri string) (raw, baseline []byte) {
	s.rendMu.Lock()
	defer s.rendMu.Unlock()
	return s.renderedRaw[uri], s.renderedBaseline[uri]
}

func (s *Server) clearRenderState(uri string) {
	s.rendMu.Lock()
	delete(s.renderedDiags, uri)
	delete(s.renderedRaw, uri)
	delete(s.renderedBaseline, uri)
	s.rendMu.Unlock()
}
