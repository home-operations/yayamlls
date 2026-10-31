package lint_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/home-operations/yayamlls/internal/lint"
)

const personSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["name", "age"],
  "properties": {
    "name": { "type": "string" },
    "age": { "type": "integer" }
  },
  "additionalProperties": false
}`

// setupWorkspace writes a catalog-disabled .yayamlls.yaml plus a person.json
// schema, so the linter resolves offline. It returns the root dir.
func setupWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "person.json"), personSchema)
	mustWrite(t, filepath.Join(root, ".yayamlls.yaml"), "catalog: false\n")
	return root
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, root string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	full := append([]string{"--root", root}, args...)
	code = lint.Run(full, nil, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRun_ValidDocIsSilentAndExitsZero(t *testing.T) {
	root := setupWorkspace(t)
	doc := filepath.Join(root, "ok.yaml")
	mustWrite(t, doc, "# yaml-language-server: $schema=./person.json\nname: Alice\nage: 30\n")

	code, stdout, stderr := run(t, root, doc)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("expected no output for valid doc, got: %q", stdout)
	}
}

// A modeline above the first `---` parses as a comment-only leading
// document; it must not be validated as null against the schema.
func TestRun_ModelineAboveSeparatorNotValidatedAsNull(t *testing.T) {
	root := setupWorkspace(t)
	doc := filepath.Join(root, "ok.yaml")
	mustWrite(t, doc, "# yaml-language-server: $schema=./person.json\n---\nname: Alice\nage: 30\n")

	code, stdout, stderr := run(t, root, doc)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("expected no output, got: %q", stdout)
	}
}

func TestRun_InvalidDocReportsAndExitsOne(t *testing.T) {
	root := setupWorkspace(t)
	doc := filepath.Join(root, "bad.yaml")
	mustWrite(t, doc, "# yaml-language-server: $schema=./person.json\nname: Alice\nage: \"thirty\"\n")

	code, stdout, _ := run(t, root, doc)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "bad.yaml:3:") || !strings.Contains(stdout, "/age") {
		t.Errorf("expected an /age error at line 3, got: %q", stdout)
	}
	if !strings.Contains(stdout, ": yayamlls ") {
		t.Errorf("expected ruff-style `path:line:col: yayamlls message`, got: %q", stdout)
	}
}

func TestRun_SuppressedDocExitsZero(t *testing.T) {
	root := setupWorkspace(t)
	doc := filepath.Join(root, "suppressed.yaml")
	mustWrite(t, doc, "# yaml-language-server: $schema=./person.json\nname: Alice\nage: \"thirty\"  # yayamlls-disable-line\n")

	code, stdout, _ := run(t, root, doc)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; output: %q", code, stdout)
	}
	if stdout != "" {
		t.Errorf("expected suppressed diagnostic to be silent, got: %q", stdout)
	}
}

func TestRun_DirectoryWalkFindsInvalidFile(t *testing.T) {
	root := setupWorkspace(t)
	mustWrite(t, filepath.Join(root, "ok.yaml"), "# yaml-language-server: $schema=./person.json\nname: Alice\nage: 30\n")
	mustWrite(t, filepath.Join(root, "bad.yaml"), "# yaml-language-server: $schema=./person.json\nname: Alice\nage: \"thirty\"\n")

	code, stdout, _ := run(t, root, root)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "bad.yaml") {
		t.Errorf("expected bad.yaml in output, got: %q", stdout)
	}
	if strings.Contains(stdout, "ok.yaml") {
		t.Errorf("valid file should not appear in output, got: %q", stdout)
	}
}

func TestRun_MultiDocReportsInvalidDoc(t *testing.T) {
	root := setupWorkspace(t)
	doc := filepath.Join(root, "multi.yaml")
	mustWrite(t, doc, "# yaml-language-server: $schema=./person.json\n"+
		"name: Alice\nage: 30\n"+
		"---\n# yaml-language-server: $schema=./person.json\n"+
		"name: Bob\nage: \"bad\"\n"+
		"---\n# yaml-language-server: $schema=./person.json\n"+
		"name: Carol\nage: 40\n")

	code, stdout, _ := run(t, root, doc)
	if code != 1 {
		t.Errorf("exit code = %d, want 1; output: %q", code, stdout)
	}
	if got := strings.Count(stdout, "/age"); got != 1 {
		t.Errorf("expected exactly one /age error (only Bob is invalid), got %d: %q", got, stdout)
	}
}

func TestRun_MultiDocDedupsLoadFailureWarning(t *testing.T) {
	root := setupWorkspace(t)
	doc := filepath.Join(root, "missing.yaml")
	// Two docs both pin the same nonexistent schema; the warning must
	// collapse to one despite concurrent validation.
	mustWrite(t, doc, "# yaml-language-server: $schema=./nope.json\na: 1\n"+
		"---\n# yaml-language-server: $schema=./nope.json\nb: 2\n")

	code, stdout, _ := run(t, root, doc)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 (load failure is a warning); output: %q", code, stdout)
	}
	if got := strings.Count(stdout, "schema load failed"); got != 1 {
		t.Errorf("expected exactly one deduped load-failure warning, got %d: %q", got, stdout)
	}
}

func TestRun_NoArgsExitsTwo(t *testing.T) {
	var out, errb bytes.Buffer
	if code := lint.Run(nil, nil, &out, &errb); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "usage:") {
		t.Errorf("expected usage message, got: %q", errb.String())
	}
}

const badDoc = "# yaml-language-server: $schema=./person.json\nname: Alice\nage: \"thirty\"\n"

func TestRun_DirectoryWalkSkipsVCSAndDependencyDirs(t *testing.T) {
	root := setupWorkspace(t)
	for _, d := range []string{".git", "node_modules"} {
		if err := os.MkdirAll(filepath.Join(root, "sub", d), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(root, "sub", d, "bad.yaml"), badDoc)
	}

	code, stdout, stderr := run(t, root, root)
	if code != 0 || stdout != "" {
		t.Errorf("walk should skip .git/node_modules: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	// An explicitly passed directory is never skipped.
	code, stdout, _ = run(t, root, filepath.Join(root, "sub", "node_modules"))
	if !strings.Contains(stdout, "bad.yaml") {
		t.Errorf("explicit node_modules arg: code=%d stdout=%q", code, stdout)
	}
}

func TestRun_DedupesFilesPreservingOrder(t *testing.T) {
	root := setupWorkspace(t)
	mustWrite(t, filepath.Join(root, "bad.yaml"), badDoc)

	file := filepath.Join(root, "bad.yaml")
	_, stdout, _ := run(t, root, file, root, filepath.Join(root, ".", "bad.yaml"))
	if n := strings.Count(stdout, "bad.yaml:"); n != 1 {
		t.Errorf("expected bad.yaml reported once, got %d:\n%s", n, stdout)
	}
	if !strings.HasPrefix(stdout, file+":") {
		t.Errorf("expected the first-seen original path, got %q", stdout)
	}
}

func TestRun_UnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 000 is not enforced here")
	}
	root := setupWorkspace(t)
	mustWrite(t, filepath.Join(root, "bad.yaml"), badDoc)
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	t.Run("nested directory warns and continues", func(t *testing.T) {
		code, stdout, stderr := run(t, root, root)
		if code != 1 || !strings.Contains(stdout, "bad.yaml") {
			t.Errorf("walk should continue past locked dir: code=%d stdout=%q", code, stdout)
		}
		if !strings.Contains(stderr, "warning") || !strings.Contains(stderr, "locked") {
			t.Errorf("expected warning naming locked dir, got %q", stderr)
		}
	})
	t.Run("explicit argument exits 2", func(t *testing.T) {
		if code, _, _ := run(t, root, locked); code != 2 {
			t.Errorf("exit code = %d, want 2", code)
		}
	})
}
