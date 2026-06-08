package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(thisFile))
	bin := filepath.Join(t.TempDir(), "yayamlls")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/yayamlls")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

type rpcConn struct {
	w  io.Writer
	r  *bufio.Reader
	id int
}

func (c *rpcConn) send(method string, params any) (int, error) {
	c.id++
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.id,
		"method":  method,
		"params":  params,
	}
	return c.id, c.writeFrame(msg)
}

func (c *rpcConn) notify(method string, params any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	return c.writeFrame(msg)
}

func (c *rpcConn) writeFrame(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(c.w, header); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

func (c *rpcConn) readFrame() (map[string]any, error) {
	var length int
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ": "); ok && strings.EqualFold(k, "Content-Length") {
			length, err = strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %w", err)
			}
		}
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(buf, &out)
}

func TestInitializeHandshake(t *testing.T) {
	bin := buildBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	conn := &rpcConn{w: stdin, r: bufio.NewReader(stdout)}

	initParams := map[string]any{
		"processId":    nil,
		"rootUri":      nil,
		"capabilities": map[string]any{},
	}
	if _, err := conn.send("initialize", initParams); err != nil {
		t.Fatalf("send initialize: %v", err)
	}
	frame, err := conn.readFrame()
	if err != nil {
		t.Fatalf("read initialize response: %v\nstderr:\n%s", err, stderr.String())
	}
	result, ok := frame["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", frame)
	}
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("missing capabilities: %v", result)
	}
	if _, ok := caps["textDocumentSync"]; !ok {
		t.Errorf("textDocumentSync capability not advertised; caps=%v", caps)
	}
	wsFolders, _ := caps["workspace"].(map[string]any)
	folders, _ := wsFolders["workspaceFolders"].(map[string]any)
	if folders["supported"] != true {
		t.Errorf("workspace folder support not advertised; caps=%v", caps)
	}
	info, ok := result["serverInfo"].(map[string]any)
	if !ok || info["name"] != "yayamlls" {
		t.Errorf("serverInfo missing or wrong: %v", result["serverInfo"])
	}
}
