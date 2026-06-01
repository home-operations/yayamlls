package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

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

// diskLoader is a jsonschema.URLLoader for http(s) schema URLs that caches
// bodies on disk and revalidates them with ETags.
type diskLoader struct {
	client *http.Client
}

func newDiskLoader() *diskLoader {
	return &diskLoader{client: &http.Client{Timeout: fetchTimeout}}
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
			return cachedBody, nil
		}
		return l.plainGET(url)
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		_ = os.WriteFile(bodyPath, body, 0o644)
		_ = writeMeta(metaPath, cacheMeta{
			ETag:    resp.Header.Get("ETag"),
			Fetched: time.Now(),
		})
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
	return io.ReadAll(resp.Body)
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
	return os.WriteFile(p, b, 0o644)
}
