package render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// DefaultDebounce is the render debounce applied when none is configured.
const DefaultDebounce = 750 * time.Millisecond

type Pipeline struct {
	registry *Registry
	sink     Sink
	debounce time.Duration

	mu      sync.Mutex
	pending map[string]*pending
	cache   map[string]cacheEntry
}

type pending struct {
	timer  *time.Timer
	cancel context.CancelFunc
}

// cacheEntry memoizes the render for one URI's current content. Keying the
// cache by URI (not by content) bounds it to the set of open documents, so
// it can't grow without limit as a file is edited.
type cacheEntry struct {
	hash string
	out  *RenderedOutput
	err  error
}

// Sink.Notify runs on the pipeline's goroutine; implementations must be
// non-blocking.
type Sink interface {
	Notify(uri string, out *RenderedOutput, err error)
}

func NewPipeline(reg *Registry, sink Sink) *Pipeline {
	return &Pipeline{
		registry: reg,
		sink:     sink,
		debounce: DefaultDebounce,
		pending:  make(map[string]*pending),
		cache:    make(map[string]cacheEntry),
	}
}

func (p *Pipeline) SetDebounce(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.debounce = d
}

func (p *Pipeline) Schedule(doc *SourceDocument) {
	if doc == nil {
		return
	}
	r := p.registry.For(doc)
	if r == nil {
		return
	}
	hash := contentHash(doc.Text)

	p.mu.Lock()
	// Supersede any pending or in-flight render for this URI: its content is
	// now stale. Without this an older render can finish after a newer one
	// and overwrite the current diagnostics with results for old text,
	// which looks like diagnostics failing to update on edit.
	p.cancelPendingLocked(doc.URI)
	if hit, ok := p.cache[doc.URI]; ok && hit.hash == hash {
		p.mu.Unlock()
		p.sink.Notify(doc.URI, hit.out, hit.err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	var self *pending
	t := time.AfterFunc(p.debounce, func() {
		defer cancel()
		out, err := r.Render(ctx, doc)
		p.mu.Lock()
		// A newer Schedule may have replaced us while we rendered; if so,
		// discard this result so the latest content always wins.
		if p.pending[doc.URI] != self {
			p.mu.Unlock()
			return
		}
		p.cache[doc.URI] = cacheEntry{hash: hash, out: out, err: err}
		delete(p.pending, doc.URI)
		p.mu.Unlock()
		p.sink.Notify(doc.URI, out, err)
	})
	self = &pending{timer: t, cancel: cancel}
	p.pending[doc.URI] = self
	p.mu.Unlock()
}

func (p *Pipeline) Latest(uri, text string) (*RenderedOutput, bool) {
	hash := contentHash(text)
	p.mu.Lock()
	defer p.mu.Unlock()
	if hit, ok := p.cache[uri]; ok && hit.hash == hash && hit.err == nil {
		return hit.out, true
	}
	return nil, false
}

// Cancel drops a URI's pending render and cached result. Called when a
// document closes so neither map retains entries for files no longer open.
func (p *Pipeline) Cancel(uri string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelPendingLocked(uri)
	delete(p.cache, uri)
}

// cancelPendingLocked stops and drops any pending render for uri. The
// caller must hold p.mu.
func (p *Pipeline) cancelPendingLocked(uri string) {
	old := p.pending[uri]
	if old == nil {
		return
	}
	old.timer.Stop()
	if old.cancel != nil {
		old.cancel()
	}
	delete(p.pending, uri)
}

func contentHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:8])
}
