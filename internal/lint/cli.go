package lint

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/home-operations/yayamlls/internal/config"
	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/uri"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Run executes the `validate` subcommand. It resolves schemas exactly as
// the language server does, validates each path argument (directories are
// walked for *.yaml/*.yml), and prints diagnostics as
// `path:line:col: severity: message`. It returns the process exit code: 1
// if any error-severity diagnostic was reported, 2 on a usage or I/O error,
// 0 otherwise.
func Run(argv []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var root string
	flags.StringVar(&root, "root", "", "workspace root for .yayamlls.yaml (default: auto-detect)")
	if err := flags.Parse(argv); err != nil {
		return 2
	}
	if flags.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: yayamlls validate [--root dir] <file|dir>...")
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
			results[i] = validateFile(p, resolver, store, opts)
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

func validateFile(path string, resolver *schema.Resolver, store *schema.Store, opts diagnostics.Options) fileResult {
	b, err := os.ReadFile(path)
	if err != nil {
		return fileResult{errLines: []string{"yayamlls: " + err.Error()}, failed: true}
	}
	text := string(b)
	diags := Document(text, path, resolver, store, opts)
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

func formatDiagnostic(path string, d protocol.Diagnostic) string {
	return fmt.Sprintf("%s:%d:%d: %s: %s",
		path, d.Range.Start.Line+1, d.Range.Start.Character+1,
		severityLabel(severityOf(d)), d.Message)
}

func severityOf(d protocol.Diagnostic) protocol.DiagnosticSeverity {
	if d.Severity != nil {
		return *d.Severity
	}
	return protocol.DiagnosticSeverityError
}

func severityLabel(s protocol.DiagnosticSeverity) string {
	switch s {
	case protocol.DiagnosticSeverityWarning:
		return "warning"
	case protocol.DiagnosticSeverityInformation:
		return "info"
	case protocol.DiagnosticSeverityHint:
		return "hint"
	default:
		return "error"
	}
}
