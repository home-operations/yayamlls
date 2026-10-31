package uri

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestFromPath_UnixRoundTrip(t *testing.T) {
	p := "/home/x/schemas/app.json"
	u := FromPath(p)
	if u != "file:///home/x/schemas/app.json" {
		t.Fatalf("FromPath = %q", u)
	}
	if got := ToPath(u); got != p {
		t.Fatalf("ToPath = %q, want %q", got, p)
	}
}

func TestFromPath_AlwaysThreeSlash(t *testing.T) {
	// A drive-style path must gain the leading slash so the authority is
	// empty (file:///C:/... not file://C:/...).
	if u := FromPath("C:/schemas/app.json"); u != "file:///C:/schemas/app.json" {
		t.Errorf("FromPath = %q, want file:///C:/schemas/app.json", u)
	}
}

func TestToPath_NonFileURIIsEmpty(t *testing.T) {
	if got := ToPath("https://example.com/x.json"); got != "" {
		t.Errorf("ToPath = %q, want empty", got)
	}
}

func TestToPath_UnencodedHashAndQuery(t *testing.T) {
	cases := map[string]string{
		"file:///home/user/a#b.yaml":   "/home/user/a#b.yaml",
		"file:///home/user/a?b.yaml":   "/home/user/a?b.yaml",
		"file:///home/user/a%23b.yaml": "/home/user/a#b.yaml",
		"file:///home/user/plain.yaml": "/home/user/plain.yaml",
		"file:///home/user/a%20b.yaml": "/home/user/a b.yaml",
	}
	for in, want := range cases {
		if got := ToPath(in); got != want {
			t.Errorf("ToPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToPath_RemoteHost(t *testing.T) {
	got := ToPath("file://server/share/x.yaml")
	want := ""
	if runtime.GOOS == "windows" {
		want = `\\server\share\x.yaml`
	}
	if got != want {
		t.Errorf("ToPath = %q, want %q", got, want)
	}
	if got := ToPath("file://localhost/a/b.yaml"); got != filepath.FromSlash("/a/b.yaml") {
		t.Errorf("localhost host = %q", got)
	}
}
