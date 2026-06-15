package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// maxSchemaBytes caps a single fetched schema/catalog body. Real schemas are
// well under this; the limit stops an attacker-controlled `$schema` URL from
// streaming an unbounded body into memory and onto disk.
const maxSchemaBytes = 16 << 20 // 16 MiB

// CacheDir is the on-disk schema cache root. Override in tests.
var CacheDir = defaultCacheDir()

func defaultCacheDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "yayamlls", "schemas")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "yayamlls", "schemas")
	}
	return filepath.Join(home, ".cache", "yayamlls", "schemas")
}

// fetchTimeout bounds a single schema fetch. A hung host would otherwise
// block the fetch (and the schema lookup waiting on it) indefinitely.
const fetchTimeout = 30 * time.Second

// freshTTL is how long a cached schema body is served without any network
// round-trip. Within it a cold start (CLI run, editor restart) costs zero
// fetches; past it the body is revalidated with its ETag, so an unchanged
// schema still transfers nothing.
const freshTTL = time.Hour

// diskLoader is a jsonschema.URLLoader for http(s) schema URLs that caches
// bodies on disk and revalidates them with ETags.
type diskLoader struct {
	client *http.Client
}

func newDiskLoader() *diskLoader {
	return &diskLoader{client: safeHTTPClient(fetchTimeout)}
}

func (l *diskLoader) Load(url string) (any, error) {
	body, err := l.loadBytes(url)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(body))
}

type cacheMeta struct {
	ETag    string    `json:"etag,omitempty"`
	Fetched time.Time `json:"fetched"`
}

func (l *diskLoader) loadBytes(url string) ([]byte, error) {
	if err := os.MkdirAll(CacheDir, 0o755); err != nil {
		return l.plainGET(url)
	}
	bodyPath, metaPath := pathsFor(url)
	cachedBody, _ := os.ReadFile(bodyPath)
	meta, _ := readMeta(metaPath)

	if len(cachedBody) > 0 && time.Since(meta.Fetched) < freshTTL {
		return cachedBody, nil
	}

	resp, err := l.conditionalGET(url, meta.ETag)
	if err != nil {
		// Offline: prefer stale cache over failing the whole document.
		if len(cachedBody) > 0 {
			return cachedBody, nil
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		if len(cachedBody) > 0 {
			// Restart the freshness window: the origin just confirmed the
			// cached body is current.
			_ = writeMeta(metaPath, cacheMeta{ETag: meta.ETag, Fetched: time.Now()})
			return cachedBody, nil
		}
		return l.plainGET(url)
	case http.StatusOK:
		body, err := readLimited(resp.Body)
		if err != nil {
			return nil, err
		}
		// A 200 with a non-JSON body (captive portal, proxy login page, CDN
		// error page) must never be cached: the freshness TTL would then serve
		// the junk for an hour. Prefer a prior good body; else surface the error.
		if !json.Valid(body) {
			if len(cachedBody) > 0 {
				return cachedBody, nil
			}
			return nil, fmt.Errorf("%s returned a non-JSON body (%d bytes)", url, len(body))
		}
		if err := writeCacheFile(bodyPath, body); err == nil {
			_ = writeMeta(metaPath, cacheMeta{
				ETag:    resp.Header.Get("ETag"),
				Fetched: time.Now(),
			})
		}
		return body, nil
	default:
		if len(cachedBody) > 0 {
			return cachedBody, nil
		}
		return nil, errors.New(url + " returned status " + resp.Status)
	}
}

func (l *diskLoader) plainGET(url string) ([]byte, error) {
	resp, err := l.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(url + " returned status " + resp.Status)
	}
	return readLimited(resp.Body)
}

// readLimited reads at most maxSchemaBytes, returning an error rather than a
// truncated body if the source exceeds the cap.
func readLimited(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxSchemaBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxSchemaBytes {
		return nil, fmt.Errorf("schema body exceeds %d byte limit", maxSchemaBytes)
	}
	return body, nil
}

// writeCacheFile writes body to path atomically: a temp file in the same
// directory is renamed into place, so a crash or a concurrent writer can never
// leave a torn/partial body that the freshness TTL would then serve.
func writeCacheFile(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func (l *diskLoader) conditionalGET(url, etag string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	return l.client.Do(req)
}

func pathsFor(url string) (body, meta string) {
	sum := sha256.Sum256([]byte(url))
	digest := hex.EncodeToString(sum[:])
	body = filepath.Join(CacheDir, digest+".json")
	meta = filepath.Join(CacheDir, digest+".meta.json")
	return
}

func readMeta(p string) (cacheMeta, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return cacheMeta{}, err
	}
	var m cacheMeta
	return m, json.Unmarshal(b, &m)
}

func writeMeta(p string, m cacheMeta) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeCacheFile(p, b)
}
