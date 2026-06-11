// Package lens handles textDocument/codeLens.
package lens

import (
	"strings"

	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Commands attached to the lenses; the LSP layer's executeCommand
// dispatches on them.
const (
	CommandShowRendered     = "yayamlls.showRendered"
	CommandShowRenderedDiff = "yayamlls.showRenderedDiff"
)

func Lenses(uri string, parsed *yamlast.Parsed) []protocol.CodeLens {
	if parsed == nil || parsed.File == nil {
		return nil
	}
	text := parsed.Text
	var out []protocol.CodeLens
	for _, doc := range parsed.File.Docs {
		if doc == nil || doc.Body == nil {
			continue
		}
		gvk, ok := schema.DetectGVK(doc.Body)
		if !ok || !isFluxRenderable(gvk) {
			continue
		}
		r := yamlast.LocateRange(doc, "", text)
		lensRange := protocol.Range{
			Start: protocol.Position{Line: r.Start.Line, Character: 0},
			End:   protocol.Position{Line: r.Start.Line, Character: 0},
		}
		out = append(out,
			protocol.CodeLens{
				Range: lensRange,
				Command: &protocol.Command{
					Title:     "View rendered",
					Command:   CommandShowRendered,
					Arguments: []any{uri},
				},
			},
			protocol.CodeLens{
				Range: lensRange,
				Command: &protocol.Command{
					Title:     "Diff rendered",
					Command:   CommandShowRenderedDiff,
					Arguments: []any{uri},
				},
			},
		)
	}
	return out
}

func isFluxRenderable(gvk schema.GVK) bool {
	if gvk.Kind == "HelmRelease" && strings.HasPrefix(gvk.Group, "helm.toolkit.fluxcd.io") {
		return true
	}
	if gvk.Kind == "Kustomization" && strings.HasPrefix(gvk.Group, "kustomize.toolkit.fluxcd.io") {
		return true
	}
	return false
}
