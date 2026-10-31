package schema

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultCatalogURL = "https://www.schemastore.org/api/json/catalog.json"

// catalogRetryInterval is the minimum gap between a failed catalog load and the
// next attempt, so an offline start recovers without hammering the network.
const catalogRetryInterval = time.Minute

type catalogEntry struct {
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	FileMatch []string `json:"fileMatch"`
}

type catalogDoc struct {
	Schemas []catalogEntry `json:"schemas"`
}

type Catalog struct {
	URL    string
	Client *http.Client

	// retryInterval is catalogRetryInterval unless a test overrides it.
	retryInterval time.Duration

	mu       sync.Mutex
	loading  bool
	lastTry  time.Time
	onLoaded func()
	loadErr  error

	// ready flips to true only after entries is written by a successful load;
	// entries is read-only from then on.
	ready    atomic.Bool
	doneOnce sync.Once
	done     chan struct{}
	entries  []catalogEntry
}

func NewCatalog(url string) *Catalog {
	if url == "" {
		url = DefaultCatalogURL
	}
	return &Catalog{
		URL:           url,
		Client:        safeHTTPClient(10 * time.Second),
		retryInterval: catalogRetryInterval,
		done:          make(chan struct{}),
	}
}

// Wait blocks until the first background load attempt has finished (or returns
// immediately if it already has). Used by one-shot callers like the
// validate command that cannot rely on a later request re-resolving.
func (c *Catalog) Wait() {
	<-c.done
}

// Load fetches the catalog in the background so no LSP request blocks on the
// network. onLoaded, if non-nil, runs after a successful fetch. Match retries a
// failed fetch at most once per retry interval.
func (c *Catalog) Load(onLoaded func()) {
	c.mu.Lock()
	c.onLoaded = onLoaded
	c.mu.Unlock()
	c.start(false)
}

// start launches a background fetch unless one is running or the catalog is
// ready; a retry also waits out retryInterval since the last attempt.
func (c *Catalog) start(retry bool) {
	c.mu.Lock()
	if c.loading || c.ready.Load() || (retry && (c.lastTry.IsZero() || time.Since(c.lastTry) < c.retryInterval)) {
		c.mu.Unlock()
		return
	}
	c.loading = true
	c.lastTry = time.Now()
	c.mu.Unlock()

	loader := &diskLoader{client: c.Client, dir: CacheDir}
	go func() {
		entries, err := c.fetch(loader)
		c.mu.Lock()
		c.loading = false
		c.loadErr = err
		hook := c.onLoaded
		if err == nil {
			c.entries = entries
			c.ready.Store(true)
		}
		c.mu.Unlock()
		c.doneOnce.Do(func() { close(c.done) })
		if err == nil && hook != nil {
			hook()
		}
	}()
}

// Match returns the schema URL for docPath, or "" if the catalog isn't loaded.
// It never blocks; after a failure it starts a throttled retry.
func (c *Catalog) Match(docPath string) string {
	if docPath == "" {
		return ""
	}
	if !c.ready.Load() {
		c.start(true)
		return ""
	}
	// Hoist the docPath normalize+clean out of the inner loop: globMatch is
	// called up to twice per catalog entry (~thousands of patterns) and the
	// path-clean work is identical for every entry. Letting matchGlob redo it
	// per pattern is wasted CPU on the typing-latency path.
	norm, ok := normalizeForMatch(docPath)
	if !ok {
		return ""
	}
	for _, e := range c.entries {
		for _, pat := range e.FileMatch {
			if matchNormalized(pat, norm) {
				return e.URL
			}
			// Catalog patterns commonly omit a leading `**/`.
			if !startsWithStar(pat) {
				if matchNormalized("**/"+pat, norm) {
					return e.URL
				}
			}
		}
	}
	return ""
}

func startsWithStar(pat string) bool {
	return len(pat) > 0 && pat[0] == '*'
}

// fetch loads the catalog through the disk loader, which gives the ~700KB
// catalog the same caching as schema bodies: served from disk inside the
// freshness TTL, ETag-revalidated after, stale-but-usable when offline.
func (c *Catalog) fetch(loader *diskLoader) ([]catalogEntry, error) {
	body, err := loader.loadBytes(c.URL)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	var doc catalogDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return doc.Schemas, nil
}
