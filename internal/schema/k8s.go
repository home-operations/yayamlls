package schema

import (
	"fmt"
	"strings"
)

const DefaultK8sSchemaURL = "https://k8s-schemas.home-operations.com/" +
	"{group:-core}/{kind@L}_{version@L}.json"

// k8sTemplate is a parsed schema URL template. Parsing resolves every
// placeholder against the known vars and pins each operator, so Render cannot
// fail.
type k8sTemplate struct {
	nodes []node
}

// parseK8sTemplate parses spec into a render-ready template. Supported placeholders:
//
//	{group}         full api group, "" for core
//	{groupFirst}    first DNS label of the group
//	{kind}          the resource kind
//	{version}       the api version
//	{kindLower}     legacy alias for {kind@L}
//	{versionLower}  legacy alias for {version@L}
//	{groupSeg}      legacy alias for {group}{group:+/}
//	{groupFirstSeg} legacy alias for {groupFirst}{groupFirst:+-}
//
// Expression syntax (shell-like parameter expansion):
//
//	{var:-word}     "word" if var is empty, else var
//	{var:+word}     "word" if var is non-empty, else ""
//	{var@U}         var but uppercased
//	{var@L}         var but lowercased
//
// Nested expressions: an operand or word may itself be an {expression}, evaluated
// before the surrounding one, e.g. {{group:-core}@U} yields "CORE" for core.
//
// A backslash escapes the next character, dropping the backslash: \{ and \}
// yield literal braces that don't open or close an expression, \x yields x.
func parseK8sTemplate(spec string) (*k8sTemplate, error) {
	if spec == "" {
		spec = DefaultK8sSchemaURL
	}
	nodes, err := parseTemplate(spec)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if e, ok := n.(exprNode); ok {
			if err := resolveBody(e.body); err != nil {
				return nil, err
			}
		}
	}
	return &k8sTemplate{nodes: nodes}, nil
}

// Render evaluates and expands the template into a schema URL.
func (t *k8sTemplate) Render(group, version, kind string) string {
	if version == "" || kind == "" {
		return ""
	}
	var b strings.Builder
	renderNodes(&b, t.nodes, k8sVars(group, version, kind))
	return b.String()
}

// node is a parsed template fragment: either literal text or a {expression}.
type node interface{ isNode() }

// textNode is literal output between expressions.
type textNode struct{ s string }

func (textNode) isNode() {}

// exprNode is a complete {expression}.
type exprNode struct{ body *exprBody }

func (exprNode) isNode() {}

// exprBody is an {expression}: a bare operand, or "operand <expr> word".
type exprBody struct {
	expr    expression
	operand term
	word    term
	op      operator
}

// termKind discriminates a term: operand references a var by name, word is literal.
type termKind int

const (
	termOperand termKind = iota
	termWord
)

// term is an operand or word: a var name or literal, or a nested {expression}.
type term struct {
	kind   termKind
	raw    string
	nested *exprBody
}

// operator identifies a {var@op} operator.
type operator int

const (
	operatorNone operator = iota
	operatorUpper
	operatorLower
)

func k8sVars(group, version, kind string) map[string]string {
	groupFirst, _, _ := strings.Cut(group, ".")
	groupSeg := ""
	if group != "" {
		groupSeg = group + "/"
	}
	groupFirstSeg := ""
	if groupFirst != "" {
		groupFirstSeg = groupFirst + "-"
	}
	return map[string]string{
		"group":         group,
		"groupSeg":      groupSeg,
		"groupFirst":    groupFirst,
		"groupFirstSeg": groupFirstSeg,
		"kind":          kind,
		"kindLower":     strings.ToLower(kind),
		"version":       version,
		"versionLower":  strings.ToLower(version),
	}
}

var knownK8sVars = func() map[string]struct{} {
	names := make(map[string]struct{})
	for name := range k8sVars("", "", "") {
		names[name] = struct{}{}
	}
	return names
}()

func resolveBody(eb *exprBody) error {
	if err := resolveTerm(&eb.operand); err != nil {
		return err
	}
	switch eb.expr {
	case expressionNone:
		return nil
	case expressionDefault, expressionAlt:
		return resolveTerm(&eb.word)
	case expressionOperator:
		op := parseOperator(eb.word.raw)
		if op == operatorNone {
			return fmt.Errorf("undefined operator %q on {%s}", eb.word.raw, eb.operand.raw)
		}
		eb.op = op
		return nil
	default:
		return fmt.Errorf("expression %d not implemented", eb.expr)
	}
}

func resolveTerm(t *term) error {
	if t.nested != nil {
		return resolveBody(t.nested)
	}
	switch t.kind {
	case termOperand:
		if _, ok := knownK8sVars[t.raw]; !ok {
			return fmt.Errorf("unknown placeholder %q", t.raw)
		}
		return nil
	case termWord:
		t.raw = unescapeWord(t.raw)
		return nil
	default:
		return fmt.Errorf("unknown term kind %d", t.kind)
	}
}

func parseTemplate(template string) ([]node, error) {
	var nodes []node
	var b strings.Builder
	for i := 0; i < len(template); {
		switch template[i] {
		case '\\':
			if i+1 < len(template) {
				b.WriteByte(template[i+1])
				i += 2
			} else {
				i++
			}
		case '{':
			if b.Len() > 0 {
				nodes = append(nodes, textNode{s: b.String()})
				b.Reset()
			}
			end, ok := matchBraces(template, i)
			if !ok {
				return nil, fmt.Errorf("unterminated expression at %d", i)
			}
			body, err := parseBody(template[i+1 : end-1])
			if err != nil {
				return nil, fmt.Errorf("%w at %d", err, i)
			}
			nodes = append(nodes, exprNode{body: body})
			i = end
		case '}':
			return nil, fmt.Errorf("unexpected '}' at %d", i)
		default:
			b.WriteByte(template[i])
			i++
		}
	}
	if b.Len() > 0 {
		nodes = append(nodes, textNode{s: b.String()})
	}
	return nodes, nil
}

func parseBody(body string) (*exprBody, error) {
	exp, idx := findExpression(body)
	eb := &exprBody{expr: exp}
	var err error
	if exp == expressionNone {
		eb.operand, err = parseTerm(body, termOperand)
		return eb, err
	}
	eb.operand, err = parseTerm(body[:idx], termOperand)
	if err != nil {
		return nil, err
	}
	eb.word, err = parseTerm(body[idx+len(exp.separator()):], termWord)
	if err != nil {
		return nil, err
	}
	return eb, nil
}

func parseTerm(s string, kind termKind) (term, error) {
	if s != "" && s[0] == '{' {
		if end, ok := matchBraces(s, 0); ok && end == len(s) {
			nested, err := parseBody(s[1 : len(s)-1])
			return term{kind: kind, nested: nested}, err
		}
		return term{}, fmt.Errorf("unbalanced nested expression %q", s)
	}
	return term{kind: kind, raw: s}, nil
}

func parseOperator(word string) operator {
	switch word {
	case "U":
		return operatorUpper
	case "L":
		return operatorLower
	}
	return operatorNone
}

func matchBraces(s string, start int) (int, bool) {
	depth := 0
	for i := start; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

func renderNodes(b *strings.Builder, nodes []node, vars map[string]string) {
	for _, n := range nodes {
		switch n := n.(type) {
		case textNode:
			b.WriteString(n.s)
		case exprNode:
			b.WriteString(renderBody(n.body, vars))
		}
	}
}

func renderBody(eb *exprBody, vars map[string]string) string {
	switch eb.expr {
	case expressionNone:
		return renderTerm(eb.operand, vars)
	case expressionDefault:
		if val := renderTerm(eb.operand, vars); val != "" {
			return val
		}
		return renderTerm(eb.word, vars)
	case expressionAlt:
		if renderTerm(eb.operand, vars) != "" {
			return renderTerm(eb.word, vars)
		}
		return ""
	case expressionOperator:
		val := renderTerm(eb.operand, vars)
		if eb.op == operatorUpper {
			return strings.ToUpper(val)
		}
		return strings.ToLower(val)
	}
	return ""
}

// renderTerm assumes a resolved term: operand raws are keys of vars (guaranteed
// by resolveTerm), so a plain index can't miss.
func renderTerm(t term, vars map[string]string) string {
	if t.nested != nil {
		return renderBody(t.nested, vars)
	}
	if t.kind == termOperand {
		return vars[t.raw]
	}
	return t.raw
}

type expression int

const (
	expressionNone expression = iota
	expressionDefault
	expressionAlt
	expressionOperator
)

func (e expression) separator() string {
	switch e {
	case expressionDefault:
		return ":-"
	case expressionAlt:
		return ":+"
	case expressionOperator:
		return "@"
	}
	return ""
}

func findExpression(s string) (expression, int) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '{' {
			end, ok := matchBraces(s, i)
			if !ok {
				return expressionNone, -1
			}
			i = end - 1
			continue
		}
		switch s[i] {
		case ':':
			if i+1 < len(s) && s[i+1] == '-' {
				return expressionDefault, i
			}
			if i+1 < len(s) && s[i+1] == '+' {
				return expressionAlt, i
			}
		case '@':
			return expressionOperator, i
		}
	}
	return expressionNone, -1
}

func unescapeWord(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			if i < len(s) {
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
