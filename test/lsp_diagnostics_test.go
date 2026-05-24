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

func readUntilDiagnostics(c *rpcConn, deadline time.Duration) (map[string]any, error) {
	done := make(chan struct {
		f   map[string]any
		err error
	}, 1)
	go func() {
		for {
			f, err := c.readFrame()
			if err != nil {
				done <- struct {
					f   map[string]any
					err error
				}{nil, err}
				return
			}
			if f["method"] == "textDocument/publishDiagnostics" {
				done <- struct {
					f   map[string]any
					err error
				}{f, nil}
				return
			}
		}
	}()
	select {
	case r := <-done:
		return r.f, r.err
	case <-time.After(deadline):
		return nil, fmt.Errorf("timed out waiting for publishDiagnostics")
	}
}

func TestPublishesSchemaDiagnostics(t *testing.T) {
	bin := buildBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	// Use the fixture's file:// URI so the modeline's relative schema
	// path resolves against the fixture directory.
	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(thisFile))
	docPath := filepath.Join(repo, "test", "fixtures", "person-invalid.yaml")
	uri := "file://" + docPath
	body := "# yaml-language-server: $schema=./schemas/person.json\nname: Alice\nage: \"thirty\"\n"

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

	frame, err := readUntilDiagnostics(conn, 5*time.Second)
	if err != nil {
		t.Fatalf("%v (stderr: %s)", err, stderr.String())
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("missing params: %v", frame)
	}
	if params["uri"] != uri {
		t.Errorf("uri = %v, want %v", params["uri"], uri)
	}
	diags, ok := params["diagnostics"].([]any)
	if !ok {
		t.Fatalf("missing diagnostics: %v", params)
	}
	if len(diags) == 0 {
		t.Fatalf("expected at least one diagnostic, got none")
	}
	combined, _ := json.Marshal(diags)
	if !strings.Contains(string(combined), "/age") {
		t.Errorf("no diagnostic mentioned /age; got: %s", combined)
	}
}

func TestPublishesEmptyDiagnosticsArray(t *testing.T) {
	bin := buildBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(thisFile))
	docPath := filepath.Join(repo, "test", "fixtures", "person-valid.yaml")
	uri := "file://" + docPath
	body := "# yaml-language-server: $schema=./schemas/person.json\nname: Alice\nage: 30\n"

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

	frame, err := readUntilDiagnostics(conn, 5*time.Second)
	if err != nil {
		t.Fatalf("%v (stderr: %s)", err, stderr.String())
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("missing params: %v", frame)
	}
	raw, ok := params["diagnostics"]
	if !ok {
		t.Fatalf("diagnostics field missing: %v", params)
	}
	if raw == nil {
		t.Fatalf("diagnostics serialized as null; clients may keep stale entries")
	}
	diags, ok := raw.([]any)
	if !ok {
		t.Fatalf("diagnostics not an array: %T %v", raw, raw)
	}
	if len(diags) != 0 {
		t.Errorf("expected zero diagnostics for valid doc, got %d: %v", len(diags), diags)
	}
}
