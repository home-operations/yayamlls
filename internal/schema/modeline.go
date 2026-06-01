package schema

import (
	"bufio"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

func FindModelineSchemaForDoc(doc *ast.DocumentNode) string {
	if doc == nil || doc.Body == nil {
		return ""
	}
	comment := headComment(doc.Body)
	if comment == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range comment.Comments {
		if tok := c.GetToken(); tok != nil {
			b.WriteString(tok.Origin)
		}
	}
	return FindModelineSchema(b.String())
}

// headComment returns the comment group at the start of a document body.
// go-yaml attaches a leading comment to the body node for a scalar or a
// single-key mapping, but to the first entry of a multi-key mapping or
// sequence.
func headComment(body ast.Node) *ast.CommentGroupNode {
	if c := body.GetComment(); c != nil {
		return c
	}
	switch n := body.(type) {
	case *ast.MappingNode:
		if len(n.Values) > 0 {
			return n.Values[0].GetComment()
		}
	case *ast.SequenceNode:
		if len(n.Values) > 0 {
			return n.Values[0].GetComment()
		}
	}
	return nil
}

const modelinePrefix = "# yaml-language-server:"

// FindModelineSchema scans the leading comment block for
// `# yaml-language-server: $schema=<url>` and returns the URL or path.
// Stops at the first non-comment line so the directive can't be smuggled
// in via a string value.
func FindModelineSchema(text string) string {
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line == "---" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			return ""
		}
		if !strings.HasPrefix(line, modelinePrefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, modelinePrefix))
		for _, kv := range strings.Fields(rest) {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			if strings.TrimSpace(k) == "$schema" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}
