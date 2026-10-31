package lint

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v6"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// RenderedDiagnostics validates rendered manifests; rendered docs have no
// source line, so the kind/name/jsonptr is embedded in each message. Both the
// LSP server and the `validate --render` command share it so they report
// identically.
//
// src is the rendered source document: helm values-schema violations are
// anchored on its spec.values keys and always reported as errors. Other render
// failures use failureSeverity.
func RenderedDiagnostics(
	store *schema.Store, resolver *schema.Resolver, src *yamlast.Parsed,
	out *render.RenderedOutput, err error, opts diagnostics.Options, failureSeverity protocol.DiagnosticSeverity,
) []protocol.Diagnostic {
	if err != nil {
		// The renderer's external tool isn't installed; rendering is an
		// opt-in extra, so stay silent rather than redlining every Flux doc.
		if errors.Is(err, render.ErrRendererUnavailable) {
			return nil
		}
		if diags := valuesDiagnostics(src, out, err.Error(), failureSeverity); len(diags) > 0 {
			return diags
		}
		return []protocol.Diagnostic{{
			Severity: new(failureSeverity),
			Source:   new(renderSource(out)),
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
				Severity: new(protocol.DiagnosticSeverityWarning),
				Source:   new(renderSource(out)),
				Message:  fmt.Sprintf("[rendered %s/%s] decode failed: %v", m.GVK.Kind, m.Name, err),
			})
			continue
		}
		if err := sch.Validate(value); err != nil {
			if verr, ok := errors.AsType[*jsonschema.ValidationError](err); ok {
				diags = append(diags, flattenRendered(out, m, verr, opts)...)
			} else {
				diags = append(diags, protocol.Diagnostic{
					Severity: new(protocol.DiagnosticSeverityError),
					Source:   new(renderSource(out)),
					Message:  fmt.Sprintf("[rendered %s/%s] %s", m.GVK.Kind, m.Name, err.Error()),
				})
			}
		}
	}
	return diags
}

func flattenRendered(
	out *render.RenderedOutput, m render.RenderedManifest, verr *jsonschema.ValidationError, opts diagnostics.Options,
) []protocol.Diagnostic {
	src := renderSource(out)
	return diagnostics.WalkLeaves(verr, func(e *jsonschema.ValidationError) (protocol.Diagnostic, bool) {
		if diagnostics.FluxSubstituted(m.AST, e, opts) {
			return protocol.Diagnostic{}, true
		}
		loc := diagnostics.Pointer(e.InstanceLocation)
		if loc == "" {
			loc = "/"
		}
		return protocol.Diagnostic{
			Severity: new(protocol.DiagnosticSeverityError),
			Source:   &src,
			Message:  fmt.Sprintf("[rendered %s/%s @ %s] %s", m.GVK.Kind, m.Name, loc, diagnostics.Message(e)),
		}, false
	})
}

func renderSource(out *render.RenderedOutput) string {
	if out == nil || out.Provider == "" {
		return diagnostics.Source + "/render"
	}
	return diagnostics.Source + "/" + out.Provider
}

// helmValuesMarker introduces helm's values-schema failure, followed by one
// "<chart>:" header per failing chart and its "- at '<ptr>': <msg>" lines.
const helmValuesMarker = "values don't meet the specifications of the schema(s) in the following chart(s):"

var (
	helmChartHeader   = regexp.MustCompile(`^(\S+):$`)
	helmValuesFailure = regexp.MustCompile(`^\s*- at '((?:[^'\\]|\\.)*)': (.*)$`)
)

// valuesDiagnostics turns a helm values-schema failure in msg into one error
// per violation, or returns nil if msg has none. Unrecognised text, such as
// another resource's joined failure, becomes a generic render diagnostic.
func valuesDiagnostics(
	src *yamlast.Parsed, out *render.RenderedOutput, msg string, failureSeverity protocol.DiagnosticSeverity,
) []protocol.Diagnostic {
	if !strings.Contains(msg, helmValuesMarker) {
		return nil
	}
	var doc *ast.DocumentNode
	var text string
	if src != nil && src.File != nil && len(src.File.Docs) > 0 {
		doc, text = src.File.Docs[0], src.Text
	}
	rootChart, _ := yamlast.StringValueAt(doc, "/spec/chart/spec/chart")
	source := renderSource(out)
	var diags []protocol.Diagnostic
	var other []string
	inValues, prefix := false, ""
	for line := range strings.SplitSeq(msg, "\n") {
		if strings.Contains(line, helmValuesMarker) {
			inValues, prefix = true, ""
			continue
		}
		if !inValues {
			if strings.TrimSpace(line) != "" {
				other = append(other, line)
			}
			continue
		}
		if m := helmChartHeader.FindStringSubmatch(line); m != nil {
			chart := m[1]
			// helm validates a subchart against values[<subchart>], so its
			// pointers are relative to that key.
			prefix = ""
			if _, ok := yamlast.LocateKey(doc, "/spec/values", chart, text); ok && chart != rootChart {
				prefix = "/" + yamlast.EscapePointerSegment(chart)
			}
			continue
		}
		m := helmValuesFailure.FindStringSubmatch(line)
		if m == nil {
			inValues = false
			if strings.TrimSpace(line) != "" {
				other = append(other, line)
			}
			continue
		}
		ptr := unquoteHelmPointer(m[1])
		diags = append(diags, protocol.Diagnostic{
			Severity: new(protocol.DiagnosticSeverityError),
			Source:   &source,
			Message:  fmt.Sprintf("helm values at '%s': %s", prefix+ptr, m[2]),
			Range:    valuesRange(doc, "/spec/values"+prefix+ptr, text),
		})
	}
	if len(other) > 0 {
		diags = append(diags, protocol.Diagnostic{
			Severity: new(failureSeverity),
			Source:   &source,
			Message:  "render: " + strings.Join(other, "\n"),
		})
	}
	return diags
}

// valuesRange anchors ptr on its deepest key present in doc: values from chart
// defaults or valuesFrom have no key of their own.
func valuesRange(doc *ast.DocumentNode, ptr, text string) protocol.Range {
	for len(ptr) >= len("/spec/values") {
		i := strings.LastIndexByte(ptr, '/')
		parent, key := ptr[:i], yamlast.UnescapePointerSegment(ptr[i+1:])
		if r, ok := yamlast.LocateKey(doc, parent, key, text); ok {
			return r
		}
		ptr = parent
	}
	return protocol.Range{}
}

// unquoteHelmPointer reverses the Go-style quoting jsonschema applies to an
// instance location; the raw text is kept if it doesn't unquote.
func unquoteHelmPointer(s string) string {
	s = strings.ReplaceAll(s, `\'`, `'`)
	u, err := strconv.Unquote(`"` + strings.ReplaceAll(s, `"`, `\"`) + `"`)
	if err != nil {
		return s
	}
	return u
}
