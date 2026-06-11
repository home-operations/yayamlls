package completion_test

import (
	"testing"

	"github.com/home-operations/yayamlls/internal/completion"
	"github.com/home-operations/yayamlls/internal/yamlast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func snippetOpts() completion.Options { return completion.Options{Snippets: true} }

func itemByLabel(t *testing.T, list *protocol.CompletionList, label string) protocol.CompletionItem {
	t.Helper()
	if list == nil {
		t.Fatal("nil completion list")
	}
	for _, it := range list.Items {
		if it.Label == label {
			return it
		}
	}
	t.Fatalf("no item labelled %q in %+v", label, list.Items)
	return protocol.CompletionItem{}
}

func assertSnippet(t *testing.T, it protocol.CompletionItem, want string) {
	t.Helper()
	if it.InsertText == nil || *it.InsertText != want {
		t.Fatalf("InsertText = %v, want %q", strOrNil(it.InsertText), want)
	}
	if it.InsertTextFormat == nil || *it.InsertTextFormat != protocol.InsertTextFormatSnippet {
		t.Fatalf("InsertTextFormat = %v, want Snippet", it.InsertTextFormat)
	}
}

func strOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func TestCompletion_SnippetScalarProperty(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {"name": {"type": "string"}}
	}`)
	list := completion.At(yamlast.Parse([]byte("")), protocol.Position{}, sch, snippetOpts())
	assertSnippet(t, itemByLabel(t, list, "name"), "name: $1")
}

func TestCompletion_SnippetDefaultBecomesPlaceholder(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {"level": {"type": "string", "default": "info"}}
	}`)
	list := completion.At(yamlast.Parse([]byte("")), protocol.Position{}, sch, snippetOpts())
	assertSnippet(t, itemByLabel(t, list, "level"), "level: ${1:info}")
}

func TestCompletion_SnippetEscapesDefault(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {"expr": {"type": "string", "default": "a$b}c"}}
	}`)
	list := completion.At(yamlast.Parse([]byte("")), protocol.Position{}, sch, snippetOpts())
	assertSnippet(t, itemByLabel(t, list, "expr"), `expr: ${1:a\$b\}c}`)
}

func TestCompletion_SnippetObjectExpandsRequiredChildren(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"required": ["name", "image"],
				"properties": {"name": {"type":"string"}, "image": {"type":"string"}}
			}
		}
	}`)
	list := completion.At(yamlast.Parse([]byte("")), protocol.Position{}, sch, snippetOpts())
	assertSnippet(t, itemByLabel(t, list, "spec"), "spec:\n  image: $1\n  name: $2")
}

func TestCompletion_SnippetObjectWithoutRequiredOpensBlock(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {"meta": {"type": "object"}}
	}`)
	list := completion.At(yamlast.Parse([]byte("")), protocol.Position{}, sch, snippetOpts())
	assertSnippet(t, itemByLabel(t, list, "meta"), "meta:\n  $1")
}

func TestCompletion_SnippetArrayProperty(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {"args": {"type": "array", "items": {"type": "string"}}}
	}`)
	list := completion.At(yamlast.Parse([]byte("")), protocol.Position{}, sch, snippetOpts())
	assertSnippet(t, itemByLabel(t, list, "args"), "args:\n  - $1")
}

// Inside a sequence item the typed word starts past "- ", so continuation
// lines must indent relative to that, not to the line's leading whitespace.
func TestCompletion_SnippetIndentInsideSequenceItem(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {
			"containers": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {"env": {"type": "object"}}
				}
			}
		}
	}`)
	text := "containers:\n  - \n"
	list := completion.At(yamlast.Parse([]byte(text)), protocol.Position{Line: 1, Character: 4}, sch, snippetOpts())
	assertSnippet(t, itemByLabel(t, list, "env"), "env:\n    $1")
}

// Without snippet support the output stays byte-identical to the legacy
// behavior: plain "key: " insert text and no InsertTextFormat.
func TestCompletion_PlainTextWhenNoSnippetSupport(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {"name": {"type": "string"}}
	}`)
	list := completion.At(yamlast.Parse([]byte("")), protocol.Position{}, sch, completion.Options{})
	it := itemByLabel(t, list, "name")
	if it.InsertText == nil || *it.InsertText != "name: " {
		t.Fatalf("InsertText = %v, want %q", strOrNil(it.InsertText), "name: ")
	}
	if it.InsertTextFormat != nil {
		t.Fatalf("InsertTextFormat = %v, want nil", *it.InsertTextFormat)
	}
}

// The ":" trigger fires with the cursor right after the colon; the inserted
// value needs a separating space while the label stays bare for filtering.
func TestCompletion_ValueAfterBareColonGetsLeadingSpace(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {"tier": {"enum": ["bronze", "silver"]}}
	}`)
	list := completion.At(yamlast.Parse([]byte("tier:\n")), protocol.Position{Line: 0, Character: 5}, sch, completion.Options{})
	it := itemByLabel(t, list, "bronze")
	if it.InsertText == nil || *it.InsertText != " bronze" {
		t.Fatalf("InsertText = %v, want %q", strOrNil(it.InsertText), " bronze")
	}

	// With the space already typed no extra space is inserted.
	list = completion.At(yamlast.Parse([]byte("tier: \n")), protocol.Position{Line: 0, Character: 6}, sch, completion.Options{})
	it = itemByLabel(t, list, "bronze")
	if it.InsertText != nil {
		t.Fatalf("InsertText = %v, want nil after \"tier: \"", strOrNil(it.InsertText))
	}
}

// The "-" trigger fires with the cursor right after the dash; property
// inserts need a separating space.
func TestCompletion_PropertyAfterBareDashGetsLeadingSpace(t *testing.T) {
	sch := compile(t, `{
		"type": "object",
		"properties": {
			"containers": {
				"type": "array",
				"items": {"type": "object", "properties": {"name": {"type": "string"}}}
			}
		}
	}`)
	text := "containers:\n  -\n"
	list := completion.At(yamlast.Parse([]byte(text)), protocol.Position{Line: 1, Character: 3}, sch, completion.Options{})
	it := itemByLabel(t, list, "name")
	if it.InsertText == nil || *it.InsertText != " name: " {
		t.Fatalf("InsertText = %v, want %q", strOrNil(it.InsertText), " name: ")
	}
}
