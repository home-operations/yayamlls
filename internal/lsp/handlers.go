package lsp

import (
	"encoding/json"
	"net/url"

	"github.com/home-operations/yamlls/internal/completion"
	"github.com/home-operations/yamlls/internal/config"
	"github.com/home-operations/yamlls/internal/diagnostics"
	"github.com/home-operations/yamlls/internal/document"
	"github.com/home-operations/yamlls/internal/folding"
	"github.com/home-operations/yamlls/internal/hover"
	"github.com/home-operations/yamlls/internal/links"
	"github.com/home-operations/yamlls/internal/render"
	"github.com/home-operations/yamlls/internal/symbols"
	"github.com/home-operations/yamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const CommandShowRendered = "yamlls.showRendered"

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
	s.clearDiagnostics(ctx, params.TextDocument.URI)
	s.docs.Close(params.TextDocument.URI)
	return nil
}

// schemaAtCursor resolves the schema for the doc the cursor is inside,
// which can differ per doc in multi-doc files.
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
	sch, err := s.schemas.Get(ref, path)
	if err != nil {
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

func (s *Server) executeCommand(ctx *glsp.Context, params *protocol.ExecuteCommandParams) (any, error) {
	switch params.Command {
	case CommandShowRendered:
		if len(params.Arguments) == 0 {
			return nil, nil
		}
		uri, _ := params.Arguments[0].(string)
		if uri == "" {
			return nil, nil
		}
		raw := s.renderedRawFor(uri)
		if len(raw) == 0 {
			if d, ok := s.docs.Get(uri); ok {
				if src := render.AnalyzeDocument(d.URI, uriToPath(d.URI), d.Text); src != nil {
					s.pipeline.Schedule(src)
				}
			}
			return map[string]any{"yaml": ""}, nil
		}
		return map[string]any{"yaml": string(raw)}, nil
	}
	return nil, nil
}

func (s *Server) didChangeWorkspaceFolders(ctx *glsp.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
	var root string
	if added := params.Event.Added; len(added) > 0 {
		root = added[0].URI
	}
	settings := config.Settings{}
	if root != "" {
		if loaded, err := config.LoadFromWorkspace(root); err == nil {
			settings = loaded
		}
	}
	s.workspaceRoot = root
	s.applySettings(settings)
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
	s.publishWith(ctx.Notify, d)
}

func (s *Server) clearDiagnostics(ctx *glsp.Context, uri string) {
	ctx.Notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []protocol.Diagnostic{},
	})
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}

func schemaLoadDiag(err error) protocol.Diagnostic {
	sev := protocol.DiagnosticSeverityWarning
	src := diagnostics.Source
	return protocol.Diagnostic{
		Severity: &sev,
		Source:   &src,
		Message:  "schema load failed: " + err.Error(),
		Range:    protocol.Range{},
	}
}
