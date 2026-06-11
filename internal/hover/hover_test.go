package hover_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/home-operations/yayamlls/internal/hover"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestHover_ReturnsDescriptionForProperty(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	docPath := filepath.Join(repo, "test", "fixtures", "person.yaml")
	store := schema.NewStore()
	sch, err := store.Get("./schemas/person.json", docPath)
	if err != nil {
		t.Fatalf("schema compile: %v", err)
	}
	text := "name: Alice\nage: 30\n"
	pos := protocol.Position{Line: 1, Character: 5}
	h := hover.At(yamlast.Parse([]byte(text)), pos, sch)
	if h == nil {
		t.Fatalf("expected hover, got nil")
	}
	body := h.Contents.(protocol.MarkupContent).Value
	if !strings.Contains(body, "integer") {
		t.Errorf("hover body missing `integer` type hint: %q", body)
	}
}
