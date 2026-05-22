package lsp

import (
	"encoding/json"
	"sync"

	"github.com/home-operations/yamlls/internal/config"
	"github.com/home-operations/yamlls/internal/diagnostics"
	"github.com/home-operations/yamlls/internal/document"
	"github.com/home-operations/yamlls/internal/render"
	"github.com/home-operations/yamlls/internal/schema"
	"github.com/home-operations/yamlls/internal/yamlast"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const lsName = "yamlls"

type Server struct {
	docs     *document.Store
	schemas  *schema.Store
	resolver *schema.Resolver
	renderer *render.Registry
	pipeline *render.Pipeline
	handler  *protocol.Handler
	version  string

	rendMu        sync.Mutex
	renderedDiags map[string][]protocol.Diagnostic
	renderedRaw   map[string][]byte

	// connNotify is captured lazily from the first incoming request so the
	// async render pipeline can push diagnostics without holding onto a
	// per-request *glsp.Context.
	connMu     sync.Mutex
	connNotify glsp.NotifyFunc

	settingsMu    sync.Mutex
	settings      config.Settings
	workspaceRoot string
}

func New(version string, registry *render.Registry) *Server {
	if registry == nil {
		registry = render.NewRegistry()
	}
	s := &Server{
		docs:          document.NewStore(),
		schemas:       schema.NewStore(),
		resolver:      schema.NewResolver(),
		renderer:      registry,
		renderedDiags: make(map[string][]protocol.Diagnostic),
		renderedRaw:   make(map[string][]byte),
		version:       version,
	}
	s.pipeline = render.NewPipeline(registry, s)
	s.handler = &protocol.Handler{
		Initialize:  s.initialize,
		Initialized: s.initialized,
		Shutdown:    s.shutdown,
		SetTrace:    s.setTrace,

		TextDocumentDidOpen:   s.didOpen,
		TextDocumentDidChange: s.didChange,
		TextDocumentDidClose:  s.didClose,

		TextDocumentHover:          s.hover,
		TextDocumentCompletion:     s.completion,
		TextDocumentFoldingRange:   s.foldingRange,
		TextDocumentDocumentLink:   s.documentLink,
		TextDocumentDocumentSymbol: s.documentSymbol,

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
		Commands: []string{CommandShowRendered},
	}

	root := pickWorkspaceRoot(params)
	var (
		settings config.Settings
		err      error
	)
	if root != "" {
		s.workspaceRoot = root
		settings, err = config.LoadFromWorkspace(root)
		if err != nil {
			notifyShowMessage(ctx, protocol.MessageTypeWarning,
				"yamlls: failed to load .yamlls.yaml: "+err.Error())
		}
	}
	if init := settingsFromInitOptions(params.InitializationOptions); init != nil {
		settings = config.Merge(settings, *init)
	}
	s.applySettings(settings)

	return protocol.InitializeResult{
		Capabilities: caps,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    lsName,
			Version: &s.version,
		},
	}, nil
}

// pickWorkspaceRoot returns the first WorkspaceFolder URI when set, else
// falls back to deprecated rootUri.
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

func (s *Server) applySettings(settings config.Settings) {
	s.settingsMu.Lock()
	s.settings = settings
	s.settingsMu.Unlock()
	s.resolver.SetSettings(settings)
	s.renderer.Configure(settings.Renderers)
}

func (s *Server) applySettingsRaw(raw json.RawMessage) {
	settings, err := config.Parse(raw)
	if err != nil {
		return
	}
	s.applySettings(config.Merge(s.currentSettings(), settings))
}

func (s *Server) currentSettings() config.Settings {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.settings
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

func ptr[T any](v T) *T { return &v }

func (s *Server) Notify(uri string, out *render.RenderedOutput, err error) {
	diags := renderDiagnostics(s.schemas, s.resolver, out, err)

	s.rendMu.Lock()
	s.renderedDiags[uri] = diags
	if out != nil {
		s.renderedRaw[uri] = out.Raw
	}
	s.rendMu.Unlock()

	s.connMu.Lock()
	notify := s.connNotify
	s.connMu.Unlock()
	if notify == nil {
		return
	}
	d, ok := s.docs.Get(uri)
	if !ok {
		return
	}
	s.publishWith(notify, d)
}

func (s *Server) captureNotify(ctx *glsp.Context) {
	if ctx == nil || ctx.Notify == nil {
		return
	}
	s.connMu.Lock()
	if s.connNotify == nil {
		s.connNotify = ctx.Notify
	}
	s.connMu.Unlock()
}

func (s *Server) publishWith(notify glsp.NotifyFunc, d *document.Document) {
	parsed := yamlast.Parse([]byte(d.Text))
	path := uriToPath(d.URI)
	fileRef := s.resolver.Resolve(d.Text, path)

	var diags []protocol.Diagnostic
	if parsed.Err != nil {
		diags = append(diags, diagnostics.ParseErrorDiagnostic(parsed.Err))
	}

	// Load failures surface only for file-level refs (modeline, glob,
	// catalog) — per-doc auto-detect would otherwise warn on every CRD
	// missing from the configured mirror.
	loadFailures := make(map[string]bool)
	for _, doc := range parsed.Docs() {
		ref := fileRef
		userIntent := ref != ""
		if ref == "" {
			ref = s.resolver.K8sURLForNode(doc.Body)
		}
		if ref == "" {
			continue
		}
		sch, err := s.schemas.Get(ref, path)
		if err != nil {
			if userIntent && !loadFailures[ref] {
				loadFailures[ref] = true
				diags = append(diags, schemaLoadDiag(err))
			}
			continue
		}
		diags = append(diags, diagnostics.ValidateDoc(doc, sch)...)
	}

	diags = append(diags, s.renderedDiagnosticsFor(d.URI)...)
	v := protocol.UInteger(d.Version)
	notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         d.URI,
		Version:     &v,
		Diagnostics: diags,
	})
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
