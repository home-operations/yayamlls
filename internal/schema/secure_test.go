package schema

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	schemaURL := srv.URL + "/schema.json"

	if _, err := loader.loadBytes(schemaURL); err == nil {
		t.Fatal("expected error for non-JSON 200 body, got nil")
	}
	bodyPath, _ := loader.pathsFor(schemaURL)
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

	l.setRoot(root, nil)
	if _, err := l.Load(uri.FromPath(inside)); err != nil {
		t.Fatalf("in-root file should load: %v", err)
	}
	if _, err := l.Load(uri.FromPath(outside)); err == nil {
		t.Fatal("file outside trust root must be blocked")
	}

	l.setRoot(root, []string{filepath.Dir(outside)})
	if _, err := l.Load(uri.FromPath(outside)); err != nil {
		t.Fatalf("file under a trusted path should load: %v", err)
	}
	l.setRoot(root, []string{outside})
	if _, err := l.Load(uri.FromPath(outside)); err != nil {
		t.Fatalf("a trusted file path should load: %v", err)
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

func TestStore_SetTrustRootRetriesBlockedRef(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(outside, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	s.SetTrustRoot(root, nil)
	if _, err := s.Get(uri.FromPath(outside), ""); err == nil {
		t.Fatal("ref outside the trust root must be blocked")
	}
	s.SetTrustRoot(root, []string{filepath.Dir(outside)})
	if _, err := s.Get(uri.FromPath(outside), ""); err != nil {
		t.Fatalf("newly trusted ref must load without waiting out the negative cache: %v", err)
	}
	s.SetTrustRoot(root, nil)
	if _, err := s.Get(uri.FromPath(outside), ""); err == nil {
		t.Fatal("revoked ref must no longer be served from cache")
	}
}

func TestSecureFileLoader_EnforcesWithEmptyRoot(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "s.json")
	if err := os.WriteFile(f, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l := &secureFileLoader{}
	if !l.setRoot("", nil) {
		t.Fatal("first setRoot must count as a change")
	}
	if l.setRoot("", nil) {
		t.Error("identical setRoot must not count as a change")
	}
	_, err := l.Load(uri.FromPath(f))
	if err == nil {
		t.Fatal("empty root must not allow arbitrary files")
	}
	if strings.Contains(err.Error(), "outside the workspace root") {
		t.Errorf("message should not assume a root: %v", err)
	}
	l.setRoot("", []string{dir})
	if _, err := l.Load(uri.FromPath(f)); err != nil {
		t.Fatalf("trusted path must load with empty root: %v", err)
	}
}

func TestStore_SetTrustRootEmptyRootEnforcesAndClears(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(f, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if _, err := s.Get(uri.FromPath(f), ""); err != nil {
		t.Fatalf("unenforced store must load: %v", err)
	}
	s.SetTrustRoot("", nil)
	if _, err := s.Get(uri.FromPath(f), ""); err == nil {
		t.Fatal("enforcing with no root must block and drop the cached compile")
	}
}

func TestIsBlockedIP_Ranges(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"100.64.0.1", true},
		{"100.100.100.200", true},
		{"100.127.255.255", true},
		{"100.128.0.1", false},
		{"0.1.2.3", true},
		{"192.0.0.8", true},
		{"192.0.2.1", false},
		{"198.18.0.1", true},
		{"198.19.255.255", true},
		{"198.20.0.1", false},
		{"fc00::1", true},
		{"fd12::1", true},
		{"224.0.0.1", true},
		{"239.1.1.1", true},
		{"ff02::1", true},
		{"ff0e::1", true},
		{"::ffff:127.0.0.1", true},
		{"::ffff:100.100.100.200", true},
		{"::ffff:169.254.169.254", true},
		{"::ffff:8.8.8.8", false},
		{"64:ff9b::7f00:1", true},
		{"64:ff9b::a9fe:a9fe", true},
		{"64:ff9b::808:808", false},
		{"2001:4860:4860::8888", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isBlockedIP(net.ParseIP(tt.ip)); got != tt.blocked {
				t.Errorf("isBlockedIP(%s) = %v, want %v", tt.ip, got, tt.blocked)
			}
		})
	}
}

func TestEnvProxyAddrs(t *testing.T) {
	mk := func(raw string) func(*http.Request) (*url.URL, error) {
		return func(*http.Request) (*url.URL, error) {
			if raw == "" {
				return nil, nil
			}
			return url.Parse(raw)
		}
	}
	tests := []struct {
		name, proxy string
		want        []string
	}{
		{"none", "", nil},
		{"explicit port", "http://127.0.0.1:3128", []string{"127.0.0.1:3128", "127.0.0.1:3128"}},
		{"default http port", "http://proxy.corp", []string{"proxy.corp:80", "proxy.corp:80"}},
		{"socks default", "socks5://10.0.0.1", []string{"10.0.0.1:1080", "10.0.0.1:1080"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := envProxyAddrs(mk(tt.proxy)); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGuardedDialContext_ExemptsOnlyProxyAddr(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	addr := ln.Addr().String()
	ctx := t.Context()

	// Loopback target that is not the proxy stays blocked even when a proxy
	// is configured elsewhere.
	if c, err := guardedDialContext(time.Second, []string{"10.1.2.3:3128"})(ctx, "tcp", addr); err == nil {
		_ = c.Close()
		t.Fatal("direct loopback dial must be blocked")
	}
	c, err := guardedDialContext(time.Second, []string{addr})(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("dial to the configured proxy must be allowed: %v", err)
	}
	_ = c.Close()
}
