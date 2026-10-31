package schema

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
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
// endpoint (169.254.169.254). Every direct dial is guarded, including those
// ProxyFromEnvironment sends direct (loopback, NO_PROXY). Only the proxy's own
// address is exempt; filtering proxied targets is the proxy's job.
func safeHTTPClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           guardedDialContext(timeout, envProxyAddrs(http.ProxyFromEnvironment)),
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// guardedDialContext dials through blockPrivateDial except for addresses in
// proxyAddrs, which are the exact host:port strings net/http dials for a proxy.
func guardedDialContext(timeout time.Duration, proxyAddrs []string) dialFunc {
	guarded := (&net.Dialer{Timeout: timeout, Control: blockPrivateDial}).DialContext
	direct := (&net.Dialer{Timeout: timeout}).DialContext
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if slices.Contains(proxyAddrs, addr) {
			return direct(ctx, network, addr)
		}
		return guarded(ctx, network, addr)
	}
}

// envProxyAddrs returns the proxy host:port pairs proxyFn selects for plain
// http and https targets, in the form net/http dials them (canonicalAddr in
// net/http/transport.go: the scheme's default port when the URL has none).
func envProxyAddrs(proxyFn func(*http.Request) (*url.URL, error)) []string {
	var addrs []string
	for _, target := range []string{"http://example.com", "https://example.com"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			continue
		}
		u, err := proxyFn(req)
		if err != nil || u == nil {
			continue
		}
		port := u.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443", "socks5": "1080", "socks5h": "1080"}[u.Scheme]
		}
		addrs = append(addrs, net.JoinHostPort(u.Hostname(), port))
	}
	return addrs
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

// blockedNets are reserved ranges net.IP's predicates miss; 100.64.0.0/10
// includes Alibaba Cloud metadata (100.100.100.200).
var blockedNets = func() []*net.IPNet {
	nets := make([]*net.IPNet, 0, 4)
	for _, c := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15"} {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		nets = append(nets, n)
	}
	return nets
}()

var nat64Prefix = &net.IPNet{IP: net.ParseIP("64:ff9b::"), Mask: net.CIDRMask(96, 128)}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	// NAT64 embeds an IPv4 address that a translator would connect to.
	if ip.To4() == nil && nat64Prefix.Contains(ip) {
		return isBlockedIP(ip.To16()[12:16])
	}
	return false
}

// secureFileLoader wraps the file:// loader. Once setRoot has been called, only
// files inside the root or a trusted path load, so a modeline can't read e.g.
// ~/.ssh/id_rsa. The CLI never calls setRoot.
type secureFileLoader struct {
	base jsonschema.FileLoader

	mu      sync.RWMutex
	enforce bool
	root    string
	trusted []string
}

// setRoot reports whether the trust set changed.
func (l *secureFileLoader) setRoot(root string, trusted []string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.enforce && l.root == root && slices.Equal(l.trusted, trusted) {
		return false
	}
	l.enforce = true
	l.root = root
	l.trusted = slices.Clone(trusted)
	return true
}

func (l *secureFileLoader) Load(ref string) (any, error) {
	l.mu.RLock()
	enforce, root, trusted := l.enforce, l.root, l.trusted
	l.mu.RUnlock()
	if enforce {
		path, err := l.base.ToFile(ref)
		if err != nil {
			return nil, err
		}
		inRoot := root != "" && pathWithin(root, path)
		if !inRoot && !slices.ContainsFunc(trusted, func(t string) bool { return pathWithin(t, path) }) {
			return nil, fmt.Errorf(
				"schema: file ref %q is not inside the workspace root or a trusted path; "+
					"add it or a parent directory to trustedSchemaPaths in your editor config to use it", path)
		}
	}
	return l.base.Load(ref)
}

// pathWithin reports whether path is root itself or nested under it. Both are
// cleaned and, when they exist, symlink-resolved so a symlink inside the repo
// can't redirect the loader to a file outside it.
func pathWithin(root, path string) bool {
	root = resolveExisting(filepath.Clean(root))
	path = resolveExisting(filepath.Clean(path))
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveExisting resolves symlinks in the longest existing prefix of p and
// re-appends the rest, so a not-yet-existing path compares consistently with a
// resolved root (e.g. macOS /var -> /private/var).
func resolveExisting(p string) string {
	var rest []string
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(append([]string{r}, rest...)...)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(append([]string{p}, rest...)...)
		}
		rest = append([]string{filepath.Base(p)}, rest...)
		p = parent
	}
}
