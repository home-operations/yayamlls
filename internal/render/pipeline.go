package render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

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

// Sink.Notify runs on the pipeline's goroutine — implementations must be
// non-blocking.
type Sink interface {
	Notify(uri string, out *RenderedOutput, err error)
}

func NewPipeline(reg *Registry, sink Sink) *Pipeline {
	return &Pipeline{
		registry: reg,
		sink:     sink,
		debounce: 750 * time.Millisecond,
		pending:  make(map[string]*pending),
		cache:    make(map[string]cacheEntry),
	}
}

func (p *Pipeline) SetDebounce(d time.Duration) { p.debounce = d }

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
	if hit, ok := p.cache[doc.URI]; ok && hit.hash == hash {
		p.mu.Unlock()
		p.sink.Notify(doc.URI, hit.out, hit.err)
		return
	}
	if old := p.pending[doc.URI]; old != nil {
		old.timer.Stop()
		if old.cancel != nil {
			old.cancel()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t := time.AfterFunc(p.debounce, func() {
		defer cancel()
		out, err := r.Render(ctx, doc)
		p.mu.Lock()
		p.cache[doc.URI] = cacheEntry{hash: hash, out: out, err: err}
		delete(p.pending, doc.URI)
		p.mu.Unlock()
		p.sink.Notify(doc.URI, out, err)
	})
	p.pending[doc.URI] = &pending{timer: t, cancel: cancel}
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
	if old := p.pending[uri]; old != nil {
		old.timer.Stop()
		if old.cancel != nil {
			old.cancel()
		}
		delete(p.pending, uri)
	}
	delete(p.cache, uri)
}

func contentHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:8])
}
