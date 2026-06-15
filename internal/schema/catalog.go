package schema

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultCatalogURL = "https://www.schemastore.org/api/json/catalog.json"

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

	once    sync.Once
	loaded  atomic.Bool
	done    chan struct{}
	loadErr error
	entries []catalogEntry
}

func NewCatalog(url string) *Catalog {
	if url == "" {
		url = DefaultCatalogURL
	}
	return &Catalog{
		URL:    url,
		Client: safeHTTPClient(10 * time.Second),
		done:   make(chan struct{}),
	}
}

// Wait blocks until the catalog's background Load has finished (or returns
// immediately if it already has). Used by one-shot callers like the
// validate command that cannot rely on a later request re-resolving.
func (c *Catalog) Wait() {
	<-c.done
}

// Load fetches the catalog once, in the background, so no LSP request
// goroutine ever blocks on the network. onLoaded, if non-nil, runs after
// the fetch completes so the caller can refresh results that depend on it.
func (c *Catalog) Load(onLoaded func()) {
	go c.once.Do(func() {
		c.load()
		c.loaded.Store(true)
		close(c.done)
		if onLoaded != nil {
			onLoaded()
		}
	})
}

// Match returns the schema URL for docPath, or "" if the catalog has not
// finished loading yet; it never blocks. A later request matches once the
// background Load completes.
func (c *Catalog) Match(docPath string) string {
	if docPath == "" || !c.loaded.Load() || c.loadErr != nil {
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

func (c *Catalog) load() {
	// The disk loader gives the ~700KB catalog the same caching as schema
	// bodies: served from disk inside the freshness TTL, ETag-revalidated
	// after, stale-but-usable when offline.
	loader := &diskLoader{client: c.Client}
	body, err := loader.loadBytes(c.URL)
	if err != nil {
		c.loadErr = err
		return
	}
	var doc catalogDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		c.loadErr = err
		return
	}
	c.entries = doc.Schemas
}
