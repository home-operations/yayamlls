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

// DefaultTimeout bounds a single render when none is configured. A render is
// near-instant in steady state; the deadline only guards against a source
// fetch (git/OCI) stalling on a slow or unreachable remote.
const DefaultTimeout = 30 * time.Second

type Pipeline struct {
	registry *Registry
	sink     Sink
	debounce time.Duration
	timeout  time.Duration

	mu      sync.Mutex
	pending map[string]*pending
	cache   map[string]cacheEntry
	// epoch is bumped on every InvalidateAll. A pending render captures it
	// at Schedule time and discards its result on writeback if it has changed,
	// so an in-flight render can never repopulate the cache after the
	// pipeline was asked to forget everything.
	epoch uint64
}

type pending struct {
	timer  *time.Timer
	cancel context.CancelFunc
	epoch  uint64
}

// cacheEntry memoizes the render for one URI's current content. Keying the
// cache by URI (not by content) bounds it to the set of open documents, so
// it can't grow without limit as a file is edited.
type cacheEntry struct {
	hash string
	out  *RenderedOutput
	err  error
}

// Sink.Notify runs on the render goroutine (the AfterFunc fired by Schedule
// or the calling goroutine on a cache hit). It may block on I/O — the real
// implementation does so to perform schema validation, and Schedule is only
// invoked from goroutines that aren't the message loop, so the blocking is
// bounded; new Sink implementations should likewise avoid the glsp message
// loop.
type Sink interface {
	Notify(uri string, out *RenderedOutput, err error)
}

func NewPipeline(reg *Registry, sink Sink) *Pipeline {
	return &Pipeline{
		registry: reg,
		sink:     sink,
		debounce: DefaultDebounce,
		timeout:  DefaultTimeout,
		pending:  make(map[string]*pending),
		cache:    make(map[string]cacheEntry),
	}
}

func (p *Pipeline) SetDebounce(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.debounce = d
}

func (p *Pipeline) SetTimeout(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timeout = d
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
	// Capture the inputs the AfterFunc needs under the lock, then release it
	// before calling time.AfterFunc: sync.Mutex is non-reentrant, and the
	// AfterFunc body takes the same lock for its writeback. p.epoch is read
	// here so a concurrent InvalidateAll (also under p.mu) is observed.
	epoch := p.epoch
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	self := &pending{cancel: cancel, epoch: epoch}
	self.timer = time.AfterFunc(p.debounce, func() {
		defer cancel()
		out, err := r.Render(ctx, doc)
		p.mu.Lock()
		// Two drop conditions: (a) a newer Schedule replaced us; (b) an
		// InvalidateAll bumped the epoch while we were rendering, so writing
		// back would resurrect content the caller asked to forget.
		if p.pending[doc.URI] != self || self.epoch != p.epoch {
			p.mu.Unlock()
			return
		}
		p.cache[doc.URI] = cacheEntry{hash: hash, out: out, err: err}
		delete(p.pending, doc.URI)
		p.mu.Unlock()
		p.sink.Notify(doc.URI, out, err)
	})
	p.mu.Lock()
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

// InvalidateAll drops every cached render result, forcing the next Schedule
// to re-render even unchanged document content. Used when a watched
// workspace file changes, since renders depend on the on-disk tree. The
// epoch bump also drops any in-flight render result: a pending writeback
// checks it on completion and discards the output if a newer InvalidateAll
// arrived while the render was running, so the cache stays empty until a
// fresh Schedule succeeds.
func (p *Pipeline) InvalidateAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.cache)
	p.epoch++
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
