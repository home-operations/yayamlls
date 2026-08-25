package schema

import (
	"fmt"
	"strings"
)

const DefaultK8sSchemaURL = "https://k8s-schemas.home-operations.com/" +
	"{group:-core}/{kind@L}_{version@L}.json"

// BuildK8sURL renders a URL template against a GVK. Supported placeholders:
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
func BuildK8sURL(template, group, version, kind string) (string, error) {
	if version == "" || kind == "" {
		return "", nil
	}
	if template == "" {
		template = DefaultK8sSchemaURL
	}
	groupFirst, _, _ := strings.Cut(group, ".")
	groupSeg := ""
	if group != "" {
		groupSeg = group + "/"
	}
	groupFirstSeg := ""
	if groupFirst != "" {
		groupFirstSeg = groupFirst + "-"
	}

	vars := map[string]string{
		"group":         group,
		"groupSeg":      groupSeg,
		"groupFirst":    groupFirst,
		"groupFirstSeg": groupFirstSeg,
		"kind":          kind,
		"kindLower":     strings.ToLower(kind),
		"version":       version,
		"versionLower":  strings.ToLower(version),
	}

	nodes, err := parseTemplate(template)
	if err != nil {
		return "", err
	}
	return evalNodes(nodes, vars)
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

func parseTemplate(template string) ([]node, error) {
	var nodes []node
	var b strings.Builder
	for i := 0; i < len(template); {
		switch template[i] {
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

func evalNodes(nodes []node, vars map[string]string) (string, error) {
	var b strings.Builder
	for _, n := range nodes {
		switch n := n.(type) {
		case textNode:
			b.WriteString(n.s)
		case exprNode:
			val, err := evalBody(n.body, vars)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
		}
	}
	return b.String(), nil
}

func evalBody(eb *exprBody, vars map[string]string) (string, error) {
	switch eb.expr {
	case expressionNone:
		return evalTerm(eb.operand, vars)
	case expressionDefault:
		if val, err := evalTerm(eb.operand, vars); err == nil && val != "" {
			return val, nil
		}
		return evalTerm(eb.word, vars)
	case expressionAlt:
		if val, err := evalTerm(eb.operand, vars); err == nil && val != "" {
			return evalTerm(eb.word, vars)
		}
		return "", nil
	case expressionOperator:
		val, err := evalTerm(eb.operand, vars)
		if err != nil {
			return "", err
		}
		switch parseOperator(eb.word.raw) {
		case operatorUpper:
			return strings.ToUpper(val), nil
		case operatorLower:
			return strings.ToLower(val), nil
		}
		return "", fmt.Errorf("undefined operator %q on {%s}", eb.word.raw, eb.operand.raw)
	default:
		return "", fmt.Errorf("expression %d not implemented", eb.expr)
	}
}

func evalTerm(t term, vars map[string]string) (string, error) {
	if t.nested != nil {
		return evalBody(t.nested, vars)
	}
	switch t.kind {
	case termOperand:
		val, ok := vars[t.raw]
		if !ok {
			return "", fmt.Errorf("unknown placeholder %q", t.raw)
		}
		return val, nil
	case termWord:
		return t.raw, nil
	default:
		return "", fmt.Errorf("unknown term kind %d", t.kind)
	}
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
