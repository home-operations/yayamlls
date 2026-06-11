package document

import (
	"fmt"
	"sync"

	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type Document struct {
	URI        string
	LanguageID string
	Version    int32
	Text       string

	// parsed caches the AST for Text. A Document is immutable after
	// construction (Apply builds a fresh one per change), so the parse
	// runs at most once per version and is shared by every consumer.
	parseOnce sync.Once
	parsed    *yamlast.Parsed
}

// Parsed lazily parses Text. Safe for concurrent use; whichever consumer
// arrives first pays the parse, the rest share the result. Consumers must
// treat the returned AST as read-only.
func (d *Document) Parsed() *yamlast.Parsed {
	d.parseOnce.Do(func() { d.parsed = yamlast.Parse([]byte(d.Text)) })
	return d.parsed
}

type Store struct {
	mu   sync.RWMutex
	docs map[string]*Document
}

func NewStore() *Store {
	return &Store{docs: make(map[string]*Document)}
}

func (s *Store) Open(uri, langID string, version int32, text string) *Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := &Document{URI: uri, LanguageID: langID, Version: version, Text: text}
	s.docs[uri] = d
	return d
}

func (s *Store) Close(uri string) {
	s.mu.Lock()
	delete(s.docs, uri)
	s.mu.Unlock()
}

func (s *Store) Get(uri string) (*Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.docs[uri]
	return d, ok
}

func (s *Store) Apply(uri string, version int32, changes []any) (*Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.docs[uri]
	if !ok {
		return nil, fmt.Errorf("document not open: %s", uri)
	}
	// Build a fresh Document rather than mutating in place: the previous
	// pointer may still be read by the async render goroutine, so an
	// in-place write to Text would be a data race.
	text := cur.Text
	for _, raw := range changes {
		switch c := raw.(type) {
		case protocol.TextDocumentContentChangeEvent:
			text = applyRangeChange(text, c)
		case protocol.TextDocumentContentChangeEventWhole:
			text = c.Text
		default:
			return nil, fmt.Errorf("unsupported change event type %T", raw)
		}
	}
	d := &Document{URI: uri, LanguageID: cur.LanguageID, Version: version, Text: text}
	s.docs[uri] = d
	return d, nil
}

func applyRangeChange(text string, c protocol.TextDocumentContentChangeEvent) string {
	start := yamlast.OffsetAt(text, c.Range.Start)
	end := yamlast.OffsetAt(text, c.Range.End)
	if start > end {
		start, end = end, start
	}
	return text[:start] + c.Text + text[end:]
}

func (s *Store) AllURIs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.docs))
	for k := range s.docs {
		out = append(out, k)
	}
	return out
}
