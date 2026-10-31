package schema

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The catalog goes through the schema disk cache: a second process-lifetime
// load inside the freshness TTL must not hit the origin at all.
func TestCatalog_LoadServedFromDiskCache(t *testing.T) {
	enableLoopbackForTest(t)
	const body = `{"schemas":[{"name":"x","url":"https://example.com/x.json","fileMatch":["x.yaml"]}]}`

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	prev := CacheDir
	CacheDir = tmp
	t.Cleanup(func() { CacheDir = prev })

	first := NewCatalog(srv.URL)
	first.Load(nil)
	first.Wait()
	if first.loadErr != nil {
		t.Fatalf("first load: %v", first.loadErr)
	}
	if got := first.Match("/tmp/x.yaml"); got != "https://example.com/x.json" {
		t.Fatalf("Match = %q", got)
	}
	if atomic.LoadInt64(&hits) != 1 {
		t.Fatalf("expected 1 origin hit, got %d", hits)
	}

	// A fresh Catalog (new process) inside the TTL loads from disk.
	second := NewCatalog(srv.URL)
	second.Load(nil)
	second.Wait()
	if second.loadErr != nil {
		t.Fatalf("second load: %v", second.loadErr)
	}
	if got := second.Match("/tmp/x.yaml"); got != "https://example.com/x.json" {
		t.Fatalf("cached Match = %q", got)
	}
	if atomic.LoadInt64(&hits) != 1 {
		t.Errorf("expected no second origin hit, got %d", hits)
	}
}

func TestCatalog_RetriesAfterFailure(t *testing.T) {
	enableLoopbackForTest(t)
	const body = `{"schemas":[{"name":"x","url":"https://example.com/x.json","fileMatch":["x.yaml"]}]}`

	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	prev := CacheDir
	CacheDir = t.TempDir()
	t.Cleanup(func() { CacheDir = prev })

	loaded := make(chan struct{}, 1)
	c := NewCatalog(srv.URL)
	c.retryInterval = time.Hour
	c.Load(func() { loaded <- struct{}{} })
	c.Wait()
	if c.Match("/tmp/x.yaml") != "" {
		t.Fatal("failed load must not match")
	}

	// Inside the interval a Match must not retry.
	fail.Store(false)
	c.Match("/tmp/x.yaml")
	time.Sleep(50 * time.Millisecond)
	if c.ready.Load() {
		t.Fatal("retry fired before the interval elapsed")
	}

	c.mu.Lock()
	c.lastTry = time.Now().Add(-2 * time.Hour)
	c.mu.Unlock()
	c.Match("/tmp/x.yaml")
	select {
	case <-loaded:
	case <-time.After(5 * time.Second):
		t.Fatal("reload hook not called after successful retry")
	}
	if got := c.Match("/tmp/x.yaml"); got != "https://example.com/x.json" {
		t.Errorf("Match after retry = %q", got)
	}
}
