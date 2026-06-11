package lint_test

import (
	"bytes"
	"os"
	"path/filepath"
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
	code = lint.Run(full, &out, &errb)
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
	if !strings.Contains(stdout, "error:") {
		t.Errorf("expected error severity label, got: %q", stdout)
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
	if code := lint.Run(nil, &out, &errb); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "usage:") {
		t.Errorf("expected usage message, got: %q", errb.String())
	}
}
