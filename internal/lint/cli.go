package lint

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/home-operations/yayamlls/internal/config"
	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/uri"
	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Run executes the `validate` subcommand. It resolves schemas exactly as
// the language server does, validates each path argument (directories are
// walked for *.yaml/*.yml), and prints diagnostics as
// `path:line:col: severity: message`. With --render it also renders Flux
// HelmRelease/Kustomization documents via the registry and validates the
// rendered output, like the editor does. It returns the process exit code: 1
// if any error-severity diagnostic was reported, 2 on a usage or I/O error,
// 0 otherwise.
func Run(argv []string, registry *render.Registry, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var root string
	var doRender bool
	flags.StringVar(&root, "root", "", "workspace root for .yayamlls.yaml (default: auto-detect)")
	flags.BoolVar(&doRender, "render", false, "render Flux HelmRelease/Kustomization docs and validate the output")
	if err := flags.Parse(argv); err != nil {
		return 2
	}
	if flags.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: yayamlls validate [--root dir] [--render] <file|dir>...")
		return 2
	}

	files, err := collectYAML(flags.Args())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "yayamlls: %v\n", err)
		return 2
	}
	if len(files) == 0 {
		_, _ = fmt.Fprintln(stderr, "yayamlls: no YAML files found")
		return 2
	}

	if root == "" {
		root = findRoot(files[0])
	}
	ws, err := config.LoadFromWorkspace(uri.FromPath(root))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "yayamlls: %v\n", err)
		return 2
	}

	resolver := schema.NewResolver()
	resolver.SetSettings(ws)
	resolver.WaitForCatalog()
	store := schema.NewStore()
	opts := diagnostics.Options{FluxSubstitutions: ws.FluxSubstitutionsEnabled(), CustomTags: ws.CustomTagNames()}

	// Rendering is opt-in: it shells out to git/helm, so plain validation
	// stays fast and offline. Wire the registry as the server does, dropping
	// command-bearing workspace renderers for the same safety reason.
	var renderer *render.Registry
	renderTimeout := render.DefaultTimeout
	if doRender && registry != nil {
		registry.SetWorkspaceRoot(root)
		trusted, dropped := config.TrustedRenderers(ws, config.Settings{})
		for _, name := range dropped {
			_, _ = fmt.Fprintf(stderr,
				"yayamlls: ignoring renderer %q: declare command-bearing renderers in editor/global config, not .yayamlls.yaml\n",
				name)
		}
		_ = registry.Configure(trusted)
		if ms := ws.RenderTimeoutMs; ms != nil && *ms > 0 {
			renderTimeout = time.Duration(*ms) * time.Millisecond
		}
		renderer = registry
	}

	// Validation is I/O-bound on schema fetches; run files concurrently so
	// distinct schemas fetch in parallel. Results are collected per index
	// and printed in input order for deterministic output.
	results := make([]fileResult, len(files))
	sem := make(chan struct{}, validateConcurrency)
	var wg sync.WaitGroup
	for i, p := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, p string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = validateFile(p, resolver, store, opts, renderer, renderTimeout)
		}(i, p)
	}
	wg.Wait()

	failed := false
	for _, r := range results {
		for _, line := range r.errLines {
			_, _ = fmt.Fprintln(stderr, line)
		}
		for _, line := range r.outLines {
			_, _ = fmt.Fprintln(stdout, line)
		}
		failed = failed || r.failed
	}
	if failed {
		return 1
	}
	return 0
}

// validateConcurrency bounds in-flight files. The work is dominated by
// network latency on schema fetches, so this is set well above core count.
const validateConcurrency = 16

type fileResult struct {
	outLines []string
	errLines []string
	failed   bool
}

func validateFile(
	path string, resolver *schema.Resolver, store *schema.Store, opts diagnostics.Options,
	renderer *render.Registry, renderTimeout time.Duration,
) fileResult {
	b, err := os.ReadFile(path)
	if err != nil {
		return fileResult{errLines: []string{"yayamlls: " + err.Error()}, failed: true}
	}
	text := string(b)
	parsed := yamlast.Parse(b)
	diags := Document(parsed, path, resolver, store, opts)

	// Validate rendered output too, when enabled. Append before filtering so a
	// `# yayamlls-disable` covers rendered findings in the same pass.
	if renderer != nil {
		if src := render.AnalyzeDocument(uri.FromPath(path), path, parsed); src != nil {
			if r := renderer.For(src); r != nil {
				ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
				out, rerr := r.Render(ctx, src)
				cancel()
				rendered := RenderedDiagnostics(store, resolver, out, rerr, opts)
				if rerr != nil {
					// A render failure is a tooling limitation, not a manifest
					// defect: warn, don't fail CI. Schema violations of rendered
					// manifests still come back as errors.
					for i := range rendered {
						rendered[i].Severity = ptr(protocol.DiagnosticSeverityWarning)
					}
				}
				diags = append(diags, rendered...)
			}
		}
	}

	diags = diagnostics.ParseSuppressions(text).Filter(diags)

	var res fileResult
	for _, d := range diags {
		res.outLines = append(res.outLines, formatDiagnostic(path, d))
		if severityOf(d) == protocol.DiagnosticSeverityError {
			res.failed = true
		}
	}
	return res
}

// collectYAML expands directory arguments into their *.yaml/*.yml files;
// explicit file arguments pass through regardless of extension.
func collectYAML(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, arg)
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && isYAML(path) {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func isYAML(p string) bool {
	ext := filepath.Ext(p)
	return ext == ".yaml" || ext == ".yml"
}

// findRoot walks up from the first file looking for .yayamlls.yaml or a git
// repository, mirroring how an editor picks the workspace root. It falls
// back to the file's own directory.
func findRoot(file string) string {
	dir := filepath.Dir(file)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	for {
		for _, marker := range []string{config.WorkspaceConfigFile, config.WorkspaceConfigFileFallback, ".git"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(file)
		}
		dir = parent
	}
}

// formatDiagnostic renders a diagnostic ruff-style, with the source (e.g.
// "yayamlls", "yayamlls/flate") in the slot ruff gives a rule code:
// `path:line:col: source message`.
func formatDiagnostic(path string, d protocol.Diagnostic) string {
	return fmt.Sprintf("%s:%d:%d: %s %s",
		path, d.Range.Start.Line+1, d.Range.Start.Character+1,
		sourceLabel(d), d.Message)
}

func sourceLabel(d protocol.Diagnostic) string {
	if d.Source != nil && *d.Source != "" {
		return *d.Source
	}
	return diagnostics.Source
}

func severityOf(d protocol.Diagnostic) protocol.DiagnosticSeverity {
	if d.Severity != nil {
		return *d.Severity
	}
	return protocol.DiagnosticSeverityError
}
