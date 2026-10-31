package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestFluxKustomizationValidates pins down the multi-schema fix from 6f458a2:
// a single-doc file that opens with `---` and a sibling modeline must resolve
// the schema, validate cleanly, and not falsely flag spec.required keys.
func TestFluxKustomizationValidates(t *testing.T) {
	bin := buildBinary(t)

	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(thisFile))
	docPath := filepath.Join(repo, "test", "fixtures", "flux-kustomization.yaml")
	uri := "file://" + docPath

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
		"rootUri":      fixturesRootURI(),
		"capabilities": map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.readFrame(); err != nil {
		t.Fatalf("init: %v (stderr=%s)", err, stderr.String())
	}
	_ = conn.notify("initialized", map[string]any{})

	body := `---
# yaml-language-server: $schema=./schemas/flux-kustomization_v1.json
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: forgejo-runner
spec:
  dependsOn:
    - name: onepassword-connect
      namespace: external-secrets
  interval: 12h
  path: ./kubernetes/apps/forgejo/forgejo-runner/app
  prune: true
  sourceRef:
    kind: GitRepository
    name: flux-system
    namespace: flux-system
  targetNamespace: forgejo
  timeout: 5m
  wait: true
`
	_ = conn.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "yaml",
			"version":    1,
			"text":       body,
		},
	})

	frame, err := readUntilDiagnostics(conn, 5*time.Second)
	if err != nil {
		t.Fatalf("%v (stderr=%s)", err, stderr.String())
	}
	params, _ := frame["params"].(map[string]any)
	diags, _ := params["diagnostics"].([]any)
	if len(diags) != 0 {
		combined, _ := json.Marshal(diags)
		// Surface the historical false positive specifically.
		if strings.Contains(string(combined), "interval") &&
			strings.Contains(string(combined), "prune") &&
			strings.Contains(string(combined), "sourceRef") {
			t.Fatalf("regression: spec.required keys falsely reported missing: %s", combined)
		}
		t.Fatalf("expected 0 diagnostics, got %d: %s", len(diags), combined)
	}
}
