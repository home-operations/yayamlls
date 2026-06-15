// Package uri converts between file:// URIs and local filesystem paths,
// handling the Windows drive-letter quirk that url.Parse leaves a leading
// slash on (file:///C:/x -> "/C:/x").
package uri

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// ToPath converts a file:// URI to a local filesystem path, or "" when the
// URI is not a file URI. On Windows it strips the leading slash before a
// drive letter and converts to backslashes so the result is a valid path.
func ToPath(uriStr string) string {
	u, err := url.Parse(uriStr)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	// Rebuild the path from escaped components so a raw '?' or '#' that a
	// non-encoding client left in the filename isn't silently dropped:
	// url.Parse would otherwise split those into Query/Fragment and truncate
	// the path. Conformant clients percent-encode them, which decodes the same.
	raw := u.EscapedPath()
	if u.RawQuery != "" {
		raw += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		raw += "#" + u.EscapedFragment()
	}
	p, err := url.PathUnescape(raw)
	if err != nil {
		p = u.Path
	}
	if runtime.GOOS == "windows" {
		if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		return filepath.FromSlash(p)
	}
	return p
}

// FromPath converts a local filesystem path to a file:// URI. A Windows
// drive path like C:\x becomes file:///C:/x (the leading slash gives the
// authority-less three-slash form file loaders expect).
func FromPath(p string) string {
	s := filepath.ToSlash(p)
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	u := url.URL{Scheme: "file", Path: s}
	return u.String()
}
