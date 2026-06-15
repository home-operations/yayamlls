package lsp

import (
	"encoding/json"
	"path/filepath"

	"github.com/home-operations/yayamlls/internal/actions"
	"github.com/home-operations/yayamlls/internal/completion"
	"github.com/home-operations/yayamlls/internal/config"
	"github.com/home-operations/yayamlls/internal/document"
	"github.com/home-operations/yayamlls/internal/folding"
	"github.com/home-operations/yayamlls/internal/hover"
	"github.com/home-operations/yayamlls/internal/lens"
	"github.com/home-operations/yayamlls/internal/links"
	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/symbols"
	fileuri "github.com/home-operations/yayamlls/internal/uri"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const (
	CommandShowRendered     = lens.CommandShowRendered
	CommandShowRenderedDiff = lens.CommandShowRenderedDiff

	resultKeyYAML = "yaml"
	resultKeyDiff = "diff"
)

func (s *Server) didOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	td := params.TextDocument
	d := s.docs.Open(td.URI, td.LanguageID, td.Version, td.Text)
	s.publishDiagnostics(ctx, d)
	return nil
}

func (s *Server) didChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	td := params.TextDocument
	if _, err := s.docs.Apply(td.URI, td.Version, params.ContentChanges); err != nil {
		return err
	}
	// captureNotify must run here: the debounce timer callback has no ctx.
	s.captureNotify(ctx)
	s.debouncePublish(td.URI)
	return nil
}

func (s *Server) didClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI
	s.cancelDebounce(uri)
	s.forgetPublish(uri)
	s.clearDiagnostics(ctx, uri)
	s.docs.Close(uri)
	s.pipeline.Cancel(uri)
	s.clearRenderState(uri)
	s.takePendingShow(uri)
	removeRenderTemps(uri)
	return nil
}

// schemaAtCursor resolves the schema for the doc the cursor is inside, which
// can differ per doc in multi-doc files. It reads only the compiled-schema
// cache, never fetching: these handlers run on the message loop, and the
// diagnostics path already triggers the fetch.
func (s *Server) schemaAtCursor(uri string, pos protocol.Position) *jsonschema.Schema {
	d, ok := s.docs.Get(uri)
	if !ok {
		return nil
	}
	path := uriToPath(d.URI)
	ref := s.resolver.Resolve(d.Text, path)
	if ref == "" {
		parsed := yamlast.ForCursor(d.Parsed(), int(pos.Line))
		cur := yamlast.LocateCursor(parsed, d.Text, pos)
		if cur.Doc != nil {
			ref = s.resolver.K8sURLForNode(cur.Doc.Body)
		}
	}
	if ref == "" {
		return nil
	}
	sch, ok := s.schemas.Cached(ref, path)
	if !ok {
		// Cold start: warm the schema off the message loop so the next
		// request hits the cache. The store coalesces concurrent fetches.
		go func() { _, _ = s.schemas.Get(ref, path) }()
		return nil
	}
	return sch
}

func (s *Server) hover(ctx *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	d, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	sch := s.schemaAtCursor(params.TextDocument.URI, params.Position)
	if sch == nil {
		return nil, nil
	}
	return hover.At(d.Parsed(), params.Position, sch), nil
}

func (s *Server) completion(ctx *glsp.Context, params *protocol.CompletionParams) (any, error) {
	d, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	sch := s.schemaAtCursor(params.TextDocument.URI, params.Position)
	if sch == nil {
		return nil, nil
	}
	list := completion.At(d.Parsed(), params.Position, sch, completion.Options{Snippets: s.clientSnippets})
	if list == nil {
		return nil, nil
	}
	return list, nil
}

func (s *Server) foldingRange(ctx *glsp.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	d, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	return folding.Ranges(d.Parsed()), nil
}

func (s *Server) documentLink(ctx *glsp.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	d, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	return links.Links(d.Text), nil
}

func (s *Server) documentSymbol(ctx *glsp.Context, params *protocol.DocumentSymbolParams) (any, error) {
	d, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	return symbols.Outline(d.Parsed()), nil
}

func (s *Server) codeAction(ctx *glsp.Context, params *protocol.CodeActionParams) (any, error) {
	uri := params.TextDocument.URI
	d, ok := s.docs.Get(uri)
	if !ok {
		return nil, nil
	}
	sch := s.schemaAtCursor(uri, params.Range.Start)
	return actions.Compute(uri, d.Text, sch, params.Context.Diagnostics), nil
}

func (s *Server) codeLens(ctx *glsp.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	if !s.kubernetesEnabled() {
		return nil, nil
	}
	d, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	return lens.Lenses(d.URI, d.Parsed()), nil
}

func (s *Server) executeCommand(ctx *glsp.Context, params *protocol.ExecuteCommandParams) (any, error) {
	if !s.kubernetesEnabled() {
		return nil, nil
	}
	s.captureNotify(ctx)
	var kind string
	switch params.Command {
	case CommandShowRendered:
		kind = renderKindRendered
	case CommandShowRenderedDiff:
		kind = renderKindDiff
	default:
		return nil, nil
	}
	uri := commandURIArg(params)
	if uri == "" {
		return nil, nil
	}

	// Clients without window/showDocument (the bundled extensions) read the
	// rendered text from the result payload instead.
	if !s.clientShowDoc {
		if content, _ := s.renderedView(uri, kind); content == "" {
			s.scheduleRenderForURI(uri)
		}
		return s.renderPayload(uri, kind), nil
	}

	// Open it now if the render is ready, otherwise defer until the pipeline
	// reports back via Notify. Either way the show runs off the message loop.
	if content, _ := s.renderedView(uri, kind); content != "" {
		go s.showInEditor(s.currentCall(), uri, kind)
		return nil, nil
	}
	s.setPendingShow(uri, kind)
	s.scheduleRenderForURI(uri)
	return nil, nil
}

func commandURIArg(params *protocol.ExecuteCommandParams) string {
	if len(params.Arguments) == 0 {
		return ""
	}
	uri, _ := params.Arguments[0].(string)
	return uri
}

func (s *Server) scheduleRenderForURI(uri string) {
	if !s.kubernetesEnabled() {
		return
	}
	d, ok := s.docs.Get(uri)
	if !ok {
		return
	}
	if src := render.AnalyzeDocument(d.URI, uriToPath(d.URI), d.Parsed()); src != nil {
		s.pipeline.Schedule(src)
	}
}

func (s *Server) didChangeWorkspaceFolders(ctx *glsp.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
	added := params.Event.Added
	if len(added) == 0 {
		// Only folders were removed: keep the current workspace settings
		// rather than wiping them (and the override layer with them).
		return nil
	}
	s.settingsMu.Lock()
	s.workspaceRoot = added[0].URI
	s.settingsMu.Unlock()
	s.reloadWorkspaceConfig(ctx)
	return nil
}

// reloadWorkspaceConfig re-reads .yayamlls.yaml from the current workspace
// root into the workspace layer and republishes every open document.
func (s *Server) reloadWorkspaceConfig(ctx *glsp.Context) {
	s.settingsMu.Lock()
	root := s.workspaceRoot
	s.settingsMu.Unlock()
	loaded, err := config.LoadFromWorkspace(root)
	if err != nil {
		// A parse error (e.g. a typo saved into .yayamlls.yaml) must not
		// silently wipe the working config. Keep the prior workspace layer and
		// surface the failure, mirroring the initialize path.
		notifyShowMessage(ctx, protocol.MessageTypeWarning,
			"yayamlls: failed to reload .yayamlls.yaml (keeping previous settings): "+err.Error())
		return
	}
	s.setWorkspaceLayer(loaded)
	for _, uri := range s.docs.AllURIs() {
		if d, ok := s.docs.Get(uri); ok {
			s.publishDiagnostics(ctx, d)
		}
	}
}

// didChangeWatchedFiles reacts to external file changes: a workspace config
// edit reloads settings, and any YAML change invalidates tree-derived render
// caches so open Flux documents re-render against the new tree.
func (s *Server) didChangeWatchedFiles(ctx *glsp.Context, params *protocol.DidChangeWatchedFilesParams) error {
	s.captureNotify(ctx)
	var configChanged, treeChanged bool
	for _, ev := range params.Changes {
		path := uriToPath(ev.URI)
		switch filepath.Base(path) {
		case config.WorkspaceConfigFile, config.WorkspaceConfigFileFallback:
			configChanged = true
		default:
			s.renderer.InvalidateTree(path)
			treeChanged = true
		}
	}
	if configChanged {
		s.reloadWorkspaceConfig(ctx)
	}
	if treeChanged && s.kubernetesEnabled() {
		// Order matters: the tree generations were bumped above, so clearing
		// the pipeline's per-URI cache before re-scheduling guarantees fresh
		// renders rather than replayed stale results.
		s.pipeline.InvalidateAll()
		for _, uri := range s.docs.AllURIs() {
			s.scheduleRenderForURI(uri)
		}
	}
	return nil
}

func (s *Server) didChangeConfig(ctx *glsp.Context, params *protocol.DidChangeConfigurationParams) error {
	if params == nil || params.Settings == nil {
		return nil
	}
	b, err := json.Marshal(params.Settings)
	if err != nil {
		return nil
	}
	s.applySettingsRaw(b)
	for _, uri := range s.docs.AllURIs() {
		if d, ok := s.docs.Get(uri); ok {
			s.publishDiagnostics(ctx, d)
		}
	}
	return nil
}

func (s *Server) publishDiagnostics(ctx *glsp.Context, d *document.Document) {
	s.captureNotify(ctx)
	if s.kubernetesEnabled() {
		if src := render.AnalyzeDocument(d.URI, uriToPath(d.URI), d.Parsed()); src != nil {
			s.pipeline.Schedule(src)
		}
	}
	s.schedulePublish(d)
}

func (s *Server) clearDiagnostics(ctx *glsp.Context, uri string) {
	if ctx == nil || ctx.Notify == nil {
		return
	}
	ctx.Notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []protocol.Diagnostic{},
	})
}

func uriToPath(docURI string) string {
	return fileuri.ToPath(docURI)
}
