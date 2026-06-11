package document

import (
	"sync"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// "😀" is a single astral-plane rune that occupies two UTF-16 code units.
// Clients address columns after it in UTF-16 units, so an offset computed
// by counting runes would land one byte short and corrupt the edit.
func TestApplyRangeChange_AstralColumnsAreUTF16(t *testing.T) {
	text := "a: 😀x\n"
	c := protocol.TextDocumentContentChangeEvent{
		Range: &protocol.Range{
			Start: protocol.Position{Line: 0, Character: 5},
			End:   protocol.Position{Line: 0, Character: 6},
		},
		Text: "y",
	}
	got := applyRangeChange(text, c)
	if want := "a: 😀y\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStore_ApplyWholeAndIncrementalChanges(t *testing.T) {
	s := NewStore()
	s.Open("file:///a.yaml", "yaml", 1, "name: x\n")

	d, err := s.Apply("file:///a.yaml", 2, []any{
		protocol.TextDocumentContentChangeEventWhole{Text: "name: y\n"},
		protocol.TextDocumentContentChangeEvent{
			Range: &protocol.Range{
				Start: protocol.Position{Line: 0, Character: 6},
				End:   protocol.Position{Line: 0, Character: 7},
			},
			Text: "z",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Text != "name: z\n" {
		t.Errorf("text = %q, want %q", d.Text, "name: z\n")
	}
	if d.Version != 2 {
		t.Errorf("version = %d, want 2", d.Version)
	}
}

func TestStore_ApplyToUnopenedDocFails(t *testing.T) {
	s := NewStore()
	if _, err := s.Apply("file:///missing.yaml", 1, nil); err == nil {
		t.Fatal("expected an error for a document that was never opened")
	}
}

func TestDocument_ParsedSharedAcrossConcurrentCallers(t *testing.T) {
	s := NewStore()
	d := s.Open("file:///a.yaml", "yaml", 1, "name: x\n")

	results := make([]any, 8)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = d.Parsed()
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r != results[0] {
			t.Fatalf("Parsed() returned a different pointer at index %d", i)
		}
	}
}

func TestStore_ApplyYieldsFreshParse(t *testing.T) {
	s := NewStore()
	d1 := s.Open("file:///a.yaml", "yaml", 1, "name: x\n")
	p1 := d1.Parsed()

	d2, err := s.Apply("file:///a.yaml", 2, []any{
		protocol.TextDocumentContentChangeEventWhole{Text: "name: y\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	p2 := d2.Parsed()
	if p1 == p2 {
		t.Fatal("expected a distinct Parsed after Apply")
	}
	if p2.Text != "name: y\n" {
		t.Errorf("parsed text = %q, want %q", p2.Text, "name: y\n")
	}
}

func TestStore_CloseRemovesDocument(t *testing.T) {
	s := NewStore()
	s.Open("file:///a.yaml", "yaml", 1, "x")
	s.Close("file:///a.yaml")
	if _, ok := s.Get("file:///a.yaml"); ok {
		t.Fatal("document still present after Close")
	}
	if uris := s.AllURIs(); len(uris) != 0 {
		t.Errorf("AllURIs after Close = %v, want empty", uris)
	}
}
