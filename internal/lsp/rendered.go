package lsp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v6"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// renderDiagnostics validates rendered manifests; rendered docs have no
// source line, so the kind/name/jsonptr is embedded in each message.
func renderDiagnostics(store *schema.Store, resolver *schema.Resolver, out *render.RenderedOutput, err error, opts diagnostics.Options) []protocol.Diagnostic {
	if err != nil {
		// The renderer's external tool isn't installed; rendering is an
		// opt-in extra, so stay silent rather than redlining every Flux doc.
		if errors.Is(err, render.ErrRendererUnavailable) {
			return nil
		}
		return []protocol.Diagnostic{{
			Severity: ptr(protocol.DiagnosticSeverityError),
			Source:   ptr(renderSource(out)),
			Message:  "render: " + err.Error(),
			Range:    protocol.Range{},
		}}
	}
	if out == nil {
		return nil
	}
	var diags []protocol.Diagnostic
	for _, m := range out.Manifests {
		url := resolver.K8sURL(schema.GVK{Group: m.GVK.Group, Version: m.GVK.Version, Kind: m.GVK.Kind})
		if url == "" {
			continue
		}
		sch, err := store.Get(url, "")
		if err != nil {
			continue
		}
		value, err := yamlast.Decode(m.AST)
		if err != nil {
			diags = append(diags, protocol.Diagnostic{
				Severity: ptr(protocol.DiagnosticSeverityWarning),
				Source:   ptr(renderSource(out)),
				Message:  fmt.Sprintf("[rendered %s/%s] decode failed: %v", m.GVK.Kind, m.Name, err),
			})
			continue
		}
		if err := sch.Validate(value); err != nil {
			var verr *jsonschema.ValidationError
			if errors.As(err, &verr) {
				diags = append(diags, flattenRendered(out, m, verr, opts)...)
			} else {
				diags = append(diags, protocol.Diagnostic{
					Severity: ptr(protocol.DiagnosticSeverityError),
					Source:   ptr(renderSource(out)),
					Message:  fmt.Sprintf("[rendered %s/%s] %s", m.GVK.Kind, m.Name, err.Error()),
				})
			}
		}
	}
	return diags
}

func flattenRendered(out *render.RenderedOutput, m render.RenderedManifest, verr *jsonschema.ValidationError, opts diagnostics.Options) []protocol.Diagnostic {
	var diags []protocol.Diagnostic
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			if opts.FluxSubstitutions && diagnostics.Keyword(e) == "pattern" {
				if v, ok := yamlast.StringValueAt(m.AST, diagnostics.Pointer(e.InstanceLocation)); ok && strings.Contains(v, "${") {
					return
				}
			}
			loc := diagnostics.Pointer(e.InstanceLocation)
			if loc == "" {
				loc = "/"
			}
			diags = append(diags, protocol.Diagnostic{
				Severity: ptr(protocol.DiagnosticSeverityError),
				Source:   ptr(renderSource(out)),
				Message:  fmt.Sprintf("[rendered %s/%s @ %s] %s", m.GVK.Kind, m.Name, loc, diagnostics.Message(e)),
			})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(verr)
	return diags
}

func renderSource(out *render.RenderedOutput) string {
	if out == nil || out.Provider == "" {
		return diagnostics.Source + "/render"
	}
	return diagnostics.Source + "/" + out.Provider
}
