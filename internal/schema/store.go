package schema

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	_ "github.com/santhosh-tekuri/jsonschema/v5/httploader"
)

// negativeTTL keeps CRDs missing from the configured mirror from causing
// one network round-trip per keystroke.
const negativeTTL = 5 * time.Minute

type Store struct {
	mu       sync.Mutex
	compiled map[string]*jsonschema.Schema
	failures map[string]failure
}

type failure struct {
	err error
	at  time.Time
}

func NewStore() *Store {
	InstallDiskLoader()
	installEmbeddedLoader()
	return &Store{
		compiled: make(map[string]*jsonschema.Schema),
		failures: make(map[string]failure),
	}
}

func (s *Store) Get(ref, docPath string) (*jsonschema.Schema, error) {
	key, err := absRef(ref, docPath)
	if err != nil {
		return nil, fmt.Errorf("resolve schema ref %q: %w", ref, err)
	}
	s.mu.Lock()
	if sch, ok := s.compiled[key]; ok {
		s.mu.Unlock()
		return sch, nil
	}
	if f, ok := s.failures[key]; ok && time.Since(f.at) < negativeTTL {
		s.mu.Unlock()
		return nil, f.err
	}
	s.mu.Unlock()

	// Compile outside the lock: it may fetch the schema over the network,
	// and holding the mutex would stall every other schema lookup behind a
	// single slow host.
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	c.ExtractAnnotations = true
	sch, err := c.Compile(key)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		wrapped := fmt.Errorf("compile schema %s: %w", key, err)
		s.failures[key] = failure{err: wrapped, at: time.Now()}
		return nil, wrapped
	}
	// A concurrent caller may have compiled the same key while we were
	// fetching; either compiled schema is equivalent, so keep the winner.
	if existing, ok := s.compiled[key]; ok {
		return existing, nil
	}
	s.compiled[key] = sch
	return sch, nil
}

func absRef(ref, docPath string) (string, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") ||
		strings.HasPrefix(ref, "file://") || strings.HasPrefix(ref, "embedded://") {
		return ref, nil
	}
	if filepath.IsAbs(ref) {
		return "file://" + filepath.ToSlash(ref), nil
	}
	if docPath == "" {
		return "", fmt.Errorf("relative schema path %q has no document anchor", ref)
	}
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(docPath), ref))
	if err != nil {
		return "", err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String(), nil
}
