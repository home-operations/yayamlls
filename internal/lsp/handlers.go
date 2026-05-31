package lsp

import (
	"encoding/json"

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
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const (
	CommandShowRendered     = "yayamlls.showRendered"
	CommandShowRenderedDiff = "yayamlls.showRenderedDiff"

	resultKeyYAML = "yaml"
)

func (s *Server) didOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	td := params.TextDocument
	d := s.docs.Open(td.URI, td.LanguageID, td.Version, td.Text)
	s.publishDiagnostics(ctx, d)
	return nil
}

func (s *Server) didChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	td := params.TextDocument
	d, err := s.docs.Apply(td.URI, td.Version, params.ContentChanges)
	if err != nil {
		return err
	}
	s.publishDiagnostics(ctx, d)
	return nil
}

func (s *Server) didClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI
	s.forgetPublish(uri)
	s.clearDiagnostics(ctx, uri)
	s.docs.Close(uri)
	s.pipeline.Cancel(uri)
	s.clearRenderState(uri)
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
		parsed := yamlast.ParseForCursor(d.Text, int(pos.Line))
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
	return hover.At(d.Text, params.Position, sch), nil
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
	list := completion.At(d.Text, params.Position, sch)
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
	return folding.Ranges(d.Text), nil
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
	return symbols.Outline(d.Text), nil
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
	d, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	return lens.Lenses(d.URI, d.Text), nil
}

func (s *Server) executeCommand(ctx *glsp.Context, params *protocol.ExecuteCommandParams) (any, error) {
	switch params.Command {
	case CommandShowRendered:
		uri := commandURIArg(params)
		if uri == "" {
			return nil, nil
		}
		raw := s.renderedRawFor(uri)
		if len(raw) == 0 {
			s.scheduleRenderForURI(uri)
			return map[string]any{resultKeyYAML: ""}, nil
		}
		return map[string]any{resultKeyYAML: string(raw)}, nil
	case CommandShowRenderedDiff:
		uri := commandURIArg(params)
		if uri == "" {
			return nil, nil
		}
		baseline := s.renderedBaselineFor(uri)
		current := s.renderedRawFor(uri)
		if len(current) == 0 {
			s.scheduleRenderForURI(uri)
			return map[string]any{"diff": ""}, nil
		}
		return map[string]any{"diff": unifiedDiff(uri, baseline, current)}, nil
	}
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
	d, ok := s.docs.Get(uri)
	if !ok {
		return
	}
	if src := render.AnalyzeDocument(d.URI, uriToPath(d.URI), d.Text); src != nil {
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
	root := added[0].URI
	var ws config.Settings
	if loaded, err := config.LoadFromWorkspace(root); err == nil {
		ws = loaded
	}
	s.workspaceRoot = root
	s.setWorkspaceLayer(ws)
	for _, uri := range s.docs.AllURIs() {
		if d, ok := s.docs.Get(uri); ok {
			s.publishDiagnostics(ctx, d)
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
	if src := render.AnalyzeDocument(d.URI, uriToPath(d.URI), d.Text); src != nil {
		s.pipeline.Schedule(src)
	}
	s.schedulePublish(d)
}

func (s *Server) clearDiagnostics(ctx *glsp.Context, uri string) {
	ctx.Notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []protocol.Diagnostic{},
	})
}

func uriToPath(docURI string) string {
	return fileuri.ToPath(docURI)
}
