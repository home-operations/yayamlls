package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMultiDocMixedKinds(t *testing.T) {
	bin := buildBinary(t)

	root := t.TempDir()
	// Serve the kind schemas from disk so the test passes without network
	// access (distro package builds run sandboxed). A fetch failure on an
	// auto-detected kind yields zero diagnostics, which would look like a
	// validation bug instead of a missing schema.
	schemaDir := filepath.Join(root, "schemas")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	schemas := map[string]string{
		"namespace.json": `{}`,
		"service.json":   `{}`,
		"deployment.json": `{
  "type": "object",
  "properties": {
    "spec": {
      "type": "object",
      "properties": { "replicas": { "type": "integer" } }
    }
  }
}`,
	}
	for name, body := range schemas {
		if err := os.WriteFile(filepath.Join(schemaDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "kubernetes:\n  schemaUrl: \"file://" + schemaDir + "/{kind@L}.json\"\ncatalog: false\n"
	if err := os.WriteFile(filepath.Join(root, ".yayamlls.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	docPath := filepath.Join(root, "manifests.yaml")
	body := `---
apiVersion: v1
kind: Namespace
metadata:
  name: ok-ns
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: broken
spec:
  replicas: "not a number"
  selector:
    matchLabels: {app: x}
  template:
    metadata:
      labels: {app: x}
    spec:
      containers:
        - name: c
          image: nginx
---
apiVersion: v1
kind: Service
metadata:
  name: ok-svc
spec:
  ports:
    - port: 80
`
	if err := os.WriteFile(docPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

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
	rootURI := "file://" + root
	if _, err := conn.send("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      rootURI,
		"capabilities": map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.readFrame(); err != nil {
		t.Fatalf("init: %v (stderr=%s)", err, stderr.String())
	}
	_ = conn.notify("initialized", map[string]any{})

	docURI := "file://" + docPath
	_ = conn.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        docURI,
			"languageId": "yaml",
			"version":    1,
			"text":       body,
		},
	})

	frame, err := readUntilDiagnostics(conn, 15*time.Second)
	if err != nil {
		t.Fatalf("%v (stderr=%s)", err, stderr.String())
	}
	params, _ := frame["params"].(map[string]any)
	diags, _ := params["diagnostics"].([]any)
	combined, _ := json.Marshal(diags)
	if len(diags) != 1 {
		t.Fatalf("expected exactly 1 diagnostic for the broken Deployment, got %d: %s", len(diags), combined)
	}
	if !strings.Contains(string(combined), "/spec/replicas") {
		t.Errorf("expected /spec/replicas diagnostic, got: %s", combined)
	}
}
