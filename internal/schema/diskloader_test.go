package schema

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
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

	// Within the freshness TTL the cache is served without any round-trip.
	got, err = loader.loadBytes(url)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if string(got) != body {
		t.Errorf("cached body = %q, want %q", got, body)
	}
	if atomic.LoadInt64(&hits) != 1 {
		t.Errorf("expected 1 origin hit (fresh cache, no revalidation), got %d", hits)
	}

	bodyPath, metaPath := pathsFor(url)
	if _, err := os.Stat(bodyPath); err != nil {
		t.Errorf("body cache missing: %v", err)
	}
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("meta cache missing: %v", err)
	}

	// Past the TTL the body is revalidated (one 304 round-trip) and the
	// freshness window restarts.
	if err := writeMeta(metaPath, cacheMeta{ETag: etag, Fetched: time.Now().Add(-2 * freshTTL)}); err != nil {
		t.Fatal(err)
	}
	got, err = loader.loadBytes(url)
	if err != nil {
		t.Fatalf("stale load: %v", err)
	}
	if string(got) != body {
		t.Errorf("revalidated body = %q, want %q", got, body)
	}
	if atomic.LoadInt64(&hits) != 2 {
		t.Errorf("expected 2 origin hits after TTL expiry, got %d", hits)
	}
	meta, err := readMeta(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(meta.Fetched) > time.Minute {
		t.Errorf("freshness window not restarted on 304: fetched=%v", meta.Fetched)
	}

	// And the next load is fresh again: still no third hit.
	if _, err := loader.loadBytes(url); err != nil {
		t.Fatalf("post-304 load: %v", err)
	}
	if atomic.LoadInt64(&hits) != 2 {
		t.Errorf("expected no origin hit after 304 refresh, got %d", hits)
	}
}
