package schema

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/home-operations/yayamlls/internal/uri"
)

// enableLoopbackForTest relaxes the SSRF loopback block so a test can reach an
// httptest server on 127.0.0.1, restoring it afterwards.
func enableLoopbackForTest(t *testing.T) {
	t.Helper()
	prev := allowLoopbackFetch
	allowLoopbackFetch = true
	t.Cleanup(func() { allowLoopbackFetch = prev })
}

func TestLoadBytes_RejectsAndDoesNotCacheNonJSON(t *testing.T) {
	enableLoopbackForTest(t)
	// A captive portal / proxy login page: HTTP 200 with an HTML body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>login required</body></html>"))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	prev := CacheDir
	CacheDir = tmp
	t.Cleanup(func() { CacheDir = prev })

	loader := newDiskLoader()
	url := srv.URL + "/schema.json"

	if _, err := loader.loadBytes(url); err == nil {
		t.Fatal("expected error for non-JSON 200 body, got nil")
	}
	bodyPath, _ := pathsFor(url)
	if _, err := os.Stat(bodyPath); !os.IsNotExist(err) {
		t.Fatalf("non-JSON body must not poison the cache, but %s exists", bodyPath)
	}
}

func TestReadLimited_RejectsOversize(t *testing.T) {
	big := strings.NewReader(strings.Repeat("a", maxSchemaBytes+10))
	if _, err := readLimited(big); err == nil {
		t.Fatal("expected oversize body to be rejected")
	}
	small := strings.NewReader(`{"ok":true}`)
	if _, err := readLimited(small); err != nil {
		t.Fatalf("small body should pass: %v", err)
	}
}

func TestPathWithin(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "schema.json"), true},
		{filepath.Join(root, "sub", "dir", "s.json"), true},
		{root, true},
		{filepath.Join(root, "..", "escape.json"), false},
		{"/etc/passwd", false},
		{filepath.Dir(root), false},
	}
	for _, c := range cases {
		if got := pathWithin(root, c.path); got != c.want {
			t.Errorf("pathWithin(%q, %q) = %v, want %v", root, c.path, got, c.want)
		}
	}
}

func TestSecureFileLoader_BlocksOutsideRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "schema.json")
	if err := os.WriteFile(inside, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(outside, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &secureFileLoader{}

	// With no trust root, both load (CLI validate behaviour preserved).
	if _, err := l.Load(uri.FromPath(outside)); err != nil {
		t.Fatalf("no-root load should succeed: %v", err)
	}

	l.setRoot(root)
	if _, err := l.Load(uri.FromPath(inside)); err != nil {
		t.Fatalf("in-root file should load: %v", err)
	}
	if _, err := l.Load(uri.FromPath(outside)); err == nil {
		t.Fatal("file outside trust root must be blocked")
	}
}

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "169.254.169.254", "10.0.0.5", "192.168.1.1", "::1", "fe80::1", "0.0.0.0"}
	for _, s := range blocked {
		if !isBlockedIP(net.ParseIP(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34"}
	for _, s := range allowed {
		if isBlockedIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}
