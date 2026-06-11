package yamlast

import (
	"sync"
	"testing"

	"github.com/goccy/go-yaml/ast"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// One Parsed is now shared across lint, hover, completion, folding, and
// symbols (document.Document.Parsed). All consumers only read the AST; this
// test exercises the main read paths concurrently so `go test -race` guards
// that assumption against future goccy upgrades.
func TestParsed_ConcurrentReadOnlyConsumers(t *testing.T) {
	text := `---
base: &b
  name: web
  image: nginx
spec:
  <<: *b
  replicas: 3
  containers:
    - name: web
      ports:
        - containerPort: 80
---
kind: ConfigMap
data:
  key: value
`
	p := Parse([]byte(text))
	if p.Err != nil {
		t.Fatalf("unexpected parse error: %v", p.Err)
	}

	walk := func() {
		for _, doc := range p.Docs() {
			ast.Walk(walkFn(func(ast.Node) {}), doc)
		}
	}
	decode := func() {
		for _, doc := range p.Docs() {
			_, _ = Decode(doc)
		}
	}
	locateCursor := func() {
		_ = LocateCursor(p, text, protocol.Position{Line: 7, Character: 4})
	}
	locateRange := func() {
		for _, doc := range p.Docs() {
			_ = LocateRange(doc, "", text)
		}
	}

	var wg sync.WaitGroup
	for range 4 {
		for _, fn := range []func(){walk, decode, locateCursor, locateRange} {
			wg.Add(1)
			go func(fn func()) {
				defer wg.Done()
				fn()
			}(fn)
		}
	}
	wg.Wait()
}

type walkFn func(ast.Node)

func (f walkFn) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	f(n)
	return f
}
