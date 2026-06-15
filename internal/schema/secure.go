package schema

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// safeHTTPClient builds the client used for every schema/catalog fetch. It
// guards against SSRF: a document modeline or workspace config can point a
// `$schema`/catalog URL at an arbitrary host, and these fetches fire
// automatically when a file is opened, so a malicious repo could otherwise
// reach the developer's loopback, private networks, or the cloud-metadata
// endpoint (169.254.169.254). The dial-level block only applies to direct
// connections; through an explicit proxy the dialed address is the proxy
// itself, so target-IP filtering is impossible and egress policy is deferred
// to the proxy.
func safeHTTPClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ForceAttemptHTTP2:     true,
	}
	if !proxyConfigured() {
		tr.DialContext = (&net.Dialer{
			Timeout: timeout,
			Control: blockPrivateDial,
		}).DialContext
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

func proxyConfigured() bool {
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// allowLoopbackFetch relaxes the loopback portion of the SSRF guard so tests
// can reach an httptest server on 127.0.0.1. Production never sets it.
var allowLoopbackFetch = false

// blockPrivateDial runs after DNS resolution with the concrete address about
// to be dialed, so it also defeats DNS-rebinding to a private IP.
func blockPrivateDial(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("schema fetch: unresolved dial address %q", address)
	}
	if allowLoopbackFetch && ip.IsLoopback() {
		return nil
	}
	if isBlockedIP(ip) {
		return fmt.Errorf("schema fetch to %s blocked: loopback/private/link-local address", ip)
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// secureFileLoader wraps the bundled file:// loader and, when a trust root is
// configured, requires every loaded file to live inside it. This blocks the
// arbitrary-local-file read that an untrusted modeline `$schema=file:///...`
// (e.g. /etc/passwd, ~/.ssh/id_rsa) would otherwise perform. With no trust
// root set (the CLI validate path) the bundled loader is used unchanged.
type secureFileLoader struct {
	base jsonschema.FileLoader

	mu   sync.RWMutex
	root string
}

func (l *secureFileLoader) setRoot(root string) {
	l.mu.Lock()
	l.root = root
	l.mu.Unlock()
}

func (l *secureFileLoader) Load(url string) (any, error) {
	l.mu.RLock()
	root := l.root
	l.mu.RUnlock()
	if root != "" {
		path, err := l.base.ToFile(url)
		if err != nil {
			return nil, err
		}
		if !pathWithin(root, path) {
			return nil, fmt.Errorf(
				"schema: file ref %q is outside the workspace root and not allowed; "+
					"declare it in your editor/global config to use it", path)
		}
	}
	return l.base.Load(url)
}

// pathWithin reports whether path is root itself or nested under it. Both are
// cleaned and, when they exist, symlink-resolved so a symlink inside the repo
// can't redirect the loader to a file outside it.
func pathWithin(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
