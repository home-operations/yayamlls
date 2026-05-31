package lsp

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	fileuri "github.com/home-operations/yayamlls/internal/uri"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const (
	renderKindRendered = "rendered"
	renderKindDiff     = "diff"
)

func (s *Server) setPendingShow(uri, kind string) {
	s.showMu.Lock()
	s.pendingShow[uri] = kind
	s.showMu.Unlock()
}

func (s *Server) takePendingShow(uri string) (string, bool) {
	s.showMu.Lock()
	defer s.showMu.Unlock()
	kind, ok := s.pendingShow[uri]
	if ok {
		delete(s.pendingShow, uri)
	}
	return kind, ok
}

// showInEditor writes the rendered view to a temp file and asks the client to
// open it via window/showDocument. It runs off the message loop: glsp
// dispatches handlers serially, so blocking one on a server→client request
// would deadlock the connection waiting for its own response.
func (s *Server) showInEditor(call glsp.CallFunc, uri, kind string) {
	if call == nil {
		return
	}
	content, ext := s.renderedView(uri, kind)
	if content == "" {
		return
	}
	path, err := writeRenderTemp(uri, kind, ext, content)
	if err != nil {
		return
	}
	var res protocol.ShowDocumentResult
	call(protocol.ServerWindowShowDocument, protocol.ShowDocumentParams{
		URI:       fileuri.FromPath(path),
		TakeFocus: ptr(true),
	}, &res)
}

// renderedView returns the text for a render kind and the file extension that
// gives it the right editor filetype. Empty content means "not ready yet".
func (s *Server) renderedView(uri, kind string) (content, ext string) {
	if kind == renderKindDiff {
		current := s.renderedRawFor(uri)
		if len(current) == 0 {
			return "", ""
		}
		return unifiedDiff(uri, s.renderedBaselineFor(uri), current), ".rendered.diff"
	}
	return string(s.renderedRawFor(uri)), ".rendered.yaml"
}

// renderPayload is the legacy executeCommand return for clients without
// window/showDocument support: the bundled VSCode/Zed extensions read it.
func (s *Server) renderPayload(uri, kind string) any {
	content, _ := s.renderedView(uri, kind)
	if kind == renderKindDiff {
		return map[string]any{resultKeyDiff: content}
	}
	return map[string]any{resultKeyYAML: content}
}

func renderTempDir() string {
	return filepath.Join(os.TempDir(), "yayamlls-rendered")
}

// renderTempPath is deterministic in (uri, kind) so re-renders overwrite the
// same buffer and didClose can clean up without a registry. The source file
// stem keeps the editor's buffer name readable.
func renderTempPath(uri, kind, ext string) string {
	sum := sha256.Sum256([]byte(uri + "\x00" + kind))
	path := uriToPath(uri)
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if stem == "" || stem == "." {
		stem = "rendered"
	}
	return filepath.Join(renderTempDir(), stem+"-"+hex.EncodeToString(sum[:4])+ext)
}

func writeRenderTemp(uri, kind, ext, content string) (string, error) {
	if err := os.MkdirAll(renderTempDir(), 0o755); err != nil {
		return "", err
	}
	path := renderTempPath(uri, kind, ext)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// removeRenderTemps deletes the temp files a closed document may have produced.
func removeRenderTemps(uri string) {
	_ = os.Remove(renderTempPath(uri, renderKindRendered, ".rendered.yaml"))
	_ = os.Remove(renderTempPath(uri, renderKindDiff, ".rendered.diff"))
}
