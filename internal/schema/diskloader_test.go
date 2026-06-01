package schema

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestDiskCachedLoad_ServesFromCacheAfterFirstHit(t *testing.T) {
	const body = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`
	const etag = `"v1"`

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	prev := CacheDir
	CacheDir = tmp
	t.Cleanup(func() { CacheDir = prev })

	loader := newDiskLoader()
	url := srv.URL + "/schema.json"

	got, err := loader.loadBytes(url)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q", got, body)
	}
	if atomic.LoadInt64(&hits) != 1 {
		t.Errorf("expected 1 origin hit, got %d", hits)
	}

	got, err = loader.loadBytes(url)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if string(got) != body {
		t.Errorf("cached body = %q, want %q", got, body)
	}
	if atomic.LoadInt64(&hits) != 2 {
		t.Errorf("expected 2 origin hits (revalidation), got %d", hits)
	}

	bodyPath, metaPath := pathsFor(url)
	if _, err := os.Stat(bodyPath); err != nil {
		t.Errorf("body cache missing: %v", err)
	}
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("meta cache missing: %v", err)
	}
}
