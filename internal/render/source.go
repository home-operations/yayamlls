package render

import (
	"context"

	"github.com/goccy/go-yaml/ast"
	"github.com/home-operations/yayamlls/internal/schema"
)

type SourceDocument struct {
	URI      string
	Path     string
	Text     string
	AST      *ast.File
	Kind     string
	APIGroup string
	Name     string
}

type RenderedManifest struct {
	AST  *ast.DocumentNode
	GVK  schema.GVK
	Name string
}

// GVK is an alias for schema.GVK kept for backward source-compat with
// existing callers (e.g. the rendered.go bridge that hand-copies fields).
type GVK = schema.GVK

type RenderedOutput struct {
	Provider  string
	Manifests []RenderedManifest
	Raw       []byte
	Stderr    []byte
}

type Renderer interface {
	Name() string
	Matches(doc *SourceDocument) bool
	Render(ctx context.Context, doc *SourceDocument) (*RenderedOutput, error)
}
