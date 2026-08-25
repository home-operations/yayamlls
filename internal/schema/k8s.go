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
	operand string
	word    string
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
			nodes = append(nodes, exprNode{body: parseBody(template[i+1 : end-1])})
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

func parseBody(body string) *exprBody {
	exp, idx := findExpression(body)
	eb := &exprBody{expr: exp}
	if exp == expressionNone {
		eb.operand = body
		return eb
	}
	eb.operand = body[:idx]
	eb.word = body[idx+len(exp.separator()):]
	return eb
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
		val, ok := vars[eb.operand]
		if !ok {
			return "", fmt.Errorf("unknown placeholder %q", eb.operand)
		}
		return val, nil
	case expressionDefault:
		if val, ok := vars[eb.operand]; ok && val != "" {
			return val, nil
		}
		return eb.word, nil
	case expressionAlt:
		if val, ok := vars[eb.operand]; ok && val != "" {
			return eb.word, nil
		}
		return "", nil
	case expressionOperator:
		val, ok := vars[eb.operand]
		if !ok {
			return "", fmt.Errorf("unknown placeholder %q", eb.operand)
		}
		switch parseOperator(eb.word) {
		case operatorUpper:
			return strings.ToUpper(val), nil
		case operatorLower:
			return strings.ToLower(val), nil
		}
		return "", fmt.Errorf("undefined operator %q on {%s}", eb.word, eb.operand)
	default:
		return "", fmt.Errorf("expression %d not implemented", eb.expr)
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
