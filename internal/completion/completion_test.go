package completion_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/home-operations/yayamlls/internal/completion"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestCompletion_PropertyNamesAtKeyPosition(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	docPath := filepath.Join(repo, "test", "fixtures", "person.yaml")
	store := schema.NewStore()
	sch, err := store.Get("./schemas/person.json", docPath)
	if err != nil {
		t.Fatalf("schema compile: %v", err)
	}

	list := completion.At(yamlast.Parse(nil), protocol.Position{Line: 0, Character: 0}, sch, completion.Options{})
	if list == nil {
		t.Fatalf("expected completion list, got nil")
	}
	got := make(map[string]bool, len(list.Items))
	for _, it := range list.Items {
		got[it.Label] = true
	}
	for _, want := range []string{"name", "age", "email"} {
		if !got[want] {
			t.Errorf("missing %q in completions: %+v", want, list.Items)
		}
	}
}
