// Package lint validates YAML documents against their resolved schemas,
// independent of the LSP transport. Both the language server and the
// `yayamlls validate` command share Document so they report identically.
package lint

import (
	"sync"

	"github.com/goccy/go-yaml/ast"
	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// docConcurrency bounds how many documents of one multi-doc file validate
// at once. Validation is I/O-bound on schema fetches, so this exceeds core
// count; the Store coalesces concurrent fetches of the same schema.
const docConcurrency = 8

// Document validates a single file's parsed text. path is the on-disk path
// used for relative schema resolution. It returns the parse error (if any),
// per-document schema violations, and one schema-load failure per
// user-intended ref. It does not apply yayamlls-disable suppressions;
// callers filter, so the LSP server can suppress rendered diagnostics in
// the same pass.
func Document(
	parsed *yamlast.Parsed, path string, resolver *schema.Resolver, store *schema.Store, opts diagnostics.Options,
) []protocol.Diagnostic {
	text := parsed.Text
	fileRef := resolver.Resolve(text, path)

	diags := []protocol.Diagnostic{}
	if parsed.Err != nil {
		diags = append(diags, diagnostics.ParseErrorDiagnostic(parsed.Err))
	}

	// Documents are independent, so validate them concurrently; their slow
	// part is fetching distinct schemas. Single-doc files (the common case)
	// stay on the inline path to avoid goroutine overhead.
	docs := parsed.Docs()
	results := make([]docResult, len(docs))
	switch len(docs) {
	case 0:
	case 1:
		results[0] = validateOneDoc(docs[0], fileRef, text, path, resolver, store, opts)
	default:
		var wg sync.WaitGroup
		sem := make(chan struct{}, docConcurrency)
		for i, doc := range docs {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, doc *ast.DocumentNode) {
				defer wg.Done()
				defer func() { <-sem }()
				results[i] = validateOneDoc(doc, fileRef, text, path, resolver, store, opts)
			}(i, doc)
		}
		wg.Wait()
	}

	// Reassemble in document order, deduping load-failure warnings to one
	// per ref so concurrency doesn't change what the sequential loop emitted.
	seen := make(map[string]bool)
	for _, r := range results {
		if r.loadErr != nil {
			if !seen[r.ref] {
				seen[r.ref] = true
				diags = append(diags, SchemaLoadDiagnostic(r.loadErr))
			}
			continue
		}
		diags = append(diags, r.diags...)
	}
	return diags
}

type docResult struct {
	ref     string
	loadErr error
	diags   []protocol.Diagnostic
}

// validateOneDoc resolves and validates a single document. A load failure
// is reported only for a user-intended ref (modeline or workspace glob);
// per-doc Kubernetes auto-detect stays silent so a CRD missing from the
// mirror doesn't warn.
func validateOneDoc(
	doc *ast.DocumentNode, fileRef, text, path string,
	resolver *schema.Resolver, store *schema.Store, opts diagnostics.Options,
) docResult {
	ref := schema.FindModelineSchemaForDoc(doc)
	if ref == "" {
		ref = fileRef
	}
	userIntent := ref != ""
	if ref == "" {
		ref = resolver.K8sURLForNode(doc.Body)
	}
	if ref == "" {
		return docResult{}
	}
	sch, err := store.Get(ref, path)
	if err != nil {
		if userIntent {
			return docResult{ref: ref, loadErr: err}
		}
		return docResult{}
	}
	return docResult{diags: diagnostics.ValidateDoc(doc, sch, text, opts)}
}

// SchemaLoadDiagnostic is the file-level warning emitted when a
// user-intended schema ref fails to load.
func SchemaLoadDiagnostic(err error) protocol.Diagnostic {
	sev := protocol.DiagnosticSeverityWarning
	src := diagnostics.Source
	return protocol.Diagnostic{
		Severity: &sev,
		Source:   &src,
		Message:  "schema load failed: " + err.Error(),
		Range:    protocol.Range{},
	}
}
