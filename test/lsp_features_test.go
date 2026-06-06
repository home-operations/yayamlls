package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// startSession launches the server binary and completes the initialize
// handshake, returning a ready-to-use connection.
func startSession(t *testing.T) (*rpcConn, *bytes.Buffer) {
	t.Helper()
	bin := buildBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, bin)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Wait() })

	conn := &rpcConn{w: stdin, r: bufio.NewReader(stdout)}
	if _, err := conn.send("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      nil,
		"capabilities": map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.readFrame(); err != nil {
		t.Fatalf("init response: %v (stderr: %s)", err, stderr.String())
	}
	if err := conn.notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	return conn, &stderr
}

func openDoc(t *testing.T, conn *rpcConn, uri, body string) {
	t.Helper()
	if err := conn.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "yaml",
			"version":    1,
			"text":       body,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// awaitResponse reads frames until the response for the given request id
// arrives, skipping interleaved notifications (e.g. publishDiagnostics).
func awaitResponse(t *testing.T, conn *rpcConn, id int) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f, err := conn.readFrame()
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		got, ok := f["id"].(float64)
		if !ok || int(got) != id {
			continue
		}
		if e, ok := f["error"]; ok && e != nil {
			t.Fatalf("request %d failed: %v", id, e)
		}
		return f
	}
	t.Fatalf("timed out waiting for response to request %d", id)
	return nil
}

func request(t *testing.T, conn *rpcConn, method string, params any) map[string]any {
	t.Helper()
	id, err := conn.send(method, params)
	if err != nil {
		t.Fatal(err)
	}
	return awaitResponse(t, conn, id)
}

// TestLanguageFeatures drives one server session through hover, completion,
// code actions, folding, and document symbols.
func TestLanguageFeatures(t *testing.T) {
	conn, stderr := startSession(t)

	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(thisFile))
	fixtures := filepath.Join(repo, "test", "fixtures")

	// An invalid doc: warms the schema cache via the diagnostics path and
	// yields the diagnostic the codeAction test feeds back.
	invalidURI := "file://" + filepath.Join(fixtures, "person-invalid.yaml")
	invalidBody := "# yaml-language-server: $schema=./schemas/person.json\nname: Alice\nage: \"thirty\"\n"
	openDoc(t, conn, invalidURI, invalidBody)
	frame, err := readUntilDiagnostics(conn, 10*time.Second)
	if err != nil {
		t.Fatalf("%v (stderr: %s)", err, stderr.String())
	}
	diags := frame["params"].(map[string]any)["diagnostics"].([]any)
	if len(diags) == 0 {
		t.Fatal("expected a type diagnostic on the invalid doc")
	}
	ageDiag := diags[0].(map[string]any)

	t.Run("hover", func(t *testing.T) {
		resp := request(t, conn, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": invalidURI},
			"position":     map[string]any{"line": 2, "character": 1},
		})
		result, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("expected hover result, got %v", resp["result"])
		}
		contents, _ := json.Marshal(result["contents"])
		if !strings.Contains(string(contents), "integer") {
			t.Errorf("hover on age should mention its schema type, got: %s", contents)
		}
	})

	t.Run("codeAction", func(t *testing.T) {
		resp := request(t, conn, "textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": invalidURI},
			"range":        ageDiag["range"],
			"context":      map[string]any{"diagnostics": []any{ageDiag}},
		})
		actions, ok := resp["result"].([]any)
		if !ok || len(actions) == 0 {
			t.Fatalf("expected code actions, got %v", resp["result"])
		}
		var foundSuppress bool
		for _, a := range actions {
			if a.(map[string]any)["title"] == "Suppress this diagnostic" {
				foundSuppress = true
			}
		}
		if !foundSuppress {
			t.Errorf("no suppress action offered; got %v", actions)
		}
	})

	t.Run("completion", func(t *testing.T) {
		// A modeline-only doc: the cursor on the empty line is at the
		// document root, where property completions list the schema's keys.
		uri := "file://" + filepath.Join(fixtures, "person-empty.yaml")
		openDoc(t, conn, uri, "# yaml-language-server: $schema=./schemas/person.json\n")
		if _, err := readUntilDiagnostics(conn, 10*time.Second); err != nil {
			t.Fatalf("%v (stderr: %s)", err, stderr.String())
		}
		resp := request(t, conn, "textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 1, "character": 0},
		})
		raw, _ := json.Marshal(resp["result"])
		for _, want := range []string{"name", "age", "email"} {
			if !strings.Contains(string(raw), fmt.Sprintf("%q", want)) {
				t.Errorf("completion missing %q; got: %s", want, raw)
			}
		}
	})

	// A plain non-Kubernetes doc with nesting: folding and symbols work
	// without any schema.
	plainURI := "file://" + filepath.Join(fixtures, "plain.yaml")
	openDoc(t, conn, plainURI, "server:\n  host: localhost\n  ports:\n    - 8080\n    - 8443\n")

	t.Run("foldingRange", func(t *testing.T) {
		resp := request(t, conn, "textDocument/foldingRange", map[string]any{
			"textDocument": map[string]any{"uri": plainURI},
		})
		ranges, ok := resp["result"].([]any)
		if !ok || len(ranges) == 0 {
			t.Fatalf("expected folding ranges for nested mapping, got %v", resp["result"])
		}
	})

	t.Run("documentSymbol", func(t *testing.T) {
		resp := request(t, conn, "textDocument/documentSymbol", map[string]any{
			"textDocument": map[string]any{"uri": plainURI},
		})
		symbols, ok := resp["result"].([]any)
		if !ok || len(symbols) == 0 {
			t.Fatalf("expected document symbols, got %v", resp["result"])
		}
	})
}
