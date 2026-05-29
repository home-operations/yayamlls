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
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/santhosh-tekuri/jsonschema/v5/httploader"
)

var installDiskLoaderOnce sync.Once

// CacheDir is the on-disk schema cache root. Override in tests.
var CacheDir = defaultCacheDir()

func defaultCacheDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "yamlls", "schemas")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "yamlls", "schemas")
	}
	return filepath.Join(home, ".cache", "yamlls", "schemas")
}

// fetchTimeout bounds a single schema fetch. httploader's default client
// has no timeout, so a hung host would otherwise block the fetch — and the
// schema lookup waiting on it — indefinitely.
const fetchTimeout = 30 * time.Second

// InstallDiskLoader replaces jsonschema's http+https loaders with a
// disk-cached, ETag-revalidating variant. Idempotent.
func InstallDiskLoader() {
	installDiskLoaderOnce.Do(func() {
		httploader.Client = &http.Client{Timeout: fetchTimeout}
		jsonschema.Loaders["http"] = diskCachedLoad
		jsonschema.Loaders["https"] = diskCachedLoad
	})
}

type cacheMeta struct {
	ETag    string    `json:"etag,omitempty"`
	Fetched time.Time `json:"fetched"`
}

func diskCachedLoad(url string) (io.ReadCloser, error) {
	if err := os.MkdirAll(CacheDir, 0o755); err != nil {
		return httploader.Load(url)
	}
	bodyPath, metaPath := pathsFor(url)
	cachedBody, _ := os.ReadFile(bodyPath)
	meta, _ := readMeta(metaPath)

	resp, err := conditionalGET(url, meta.ETag)
	if err != nil {
		// Offline: prefer stale cache over failing the whole document.
		if len(cachedBody) > 0 {
			return io.NopCloser(bytes.NewReader(cachedBody)), nil
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		if len(cachedBody) > 0 {
			return io.NopCloser(bytes.NewReader(cachedBody)), nil
		}
		return httploader.Load(url)
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
		return io.NopCloser(bytes.NewReader(body)), nil
	default:
		if len(cachedBody) > 0 {
			return io.NopCloser(bytes.NewReader(cachedBody)), nil
		}
		return nil, errors.New(url + " returned status " + resp.Status)
	}
}

func conditionalGET(url, etag string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	return httploader.Client.Do(req)
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
