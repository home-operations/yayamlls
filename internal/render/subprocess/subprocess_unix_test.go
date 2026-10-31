//go:build unix

package subprocess_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/render/subprocess"
)

func newRenderer(t *testing.T, command ...string) render.Renderer {
	t.Helper()
	cfg, err := json.Marshal(map[string]any{"match": map[string]string{"kind": "ConfigMap"}, "command": command})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := subprocess.FromConfig("test", cfg)
	if !ok {
		t.Fatal("FromConfig failed")
	}
	return r
}

func TestRender_StdoutCapped(t *testing.T) {
	r := newRenderer(t, "head", "-c", "70000000", "/dev/zero")
	doc := &render.SourceDocument{Path: filepath.Join(t.TempDir(), "x.yaml"), Kind: "ConfigMap"}
	_, err := r.Render(context.Background(), doc)
	if err == nil || !strings.Contains(err.Error(), "stdout exceeds") {
		t.Fatalf("expected stdout cap error, got %v", err)
	}
}

func TestRender_RelativeCommandResolvedAgainstDocDir(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf 'apiVersion: v1\\nkind: ConfigMap\\nmetadata:\\n  name: rel\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "render.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRenderer(t, "./render.sh")
	out, err := r.Render(context.Background(), &render.SourceDocument{Path: filepath.Join(dir, "x.yaml"), Kind: "ConfigMap"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out.Manifests) != 1 || out.Manifests[0].Name != "rel" {
		t.Fatalf("unexpected manifests %+v", out.Manifests)
	}
}

func TestRender_TimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	r := newRenderer(t, "sh", "-c", "sleep 60 & echo $! > {dir}/pid; wait")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := r.Render(ctx, &render.SourceDocument{Path: filepath.Join(dir, "x.yaml"), Kind: "ConfigMap"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("grandchild survived the timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
