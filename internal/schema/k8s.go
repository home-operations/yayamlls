package schema

import (
	"regexp"
	"strings"
)

const DefaultK8sSchemaURL = "https://k8s-schemas.home-operations.com/" +
	"{group:-core}/{kind@L}_{version@L}.json"

var exprRe = regexp.MustCompile(`\{([^}]+)\}`)

func varsReplacer(m map[string]string) *strings.Replacer {
	args := make([]string, 0, len(m)*2)
	for k, v := range m {
		args = append(args, "{"+k+"}", v)
	}
	return strings.NewReplacer(args...)
}

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
func BuildK8sURL(template, group, version, kind string) string {
	if version == "" || kind == "" {
		return ""
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

	template = exprRe.ReplaceAllStringFunc(template, func(match string) string {
		inner := match[1 : len(match)-1]

		if name, word, ok := strings.Cut(inner, ":-"); ok {
			if val, exists := vars[name]; exists && val != "" {
				return val
			}
			return word
		}

		if name, word, ok := strings.Cut(inner, ":+"); ok {
			if val, exists := vars[name]; exists && val != "" {
				return word
			}
			return ""
		}

		if name, operator, ok := strings.Cut(inner, "@"); ok {
			if val, exists := vars[name]; exists {
				switch operator {
				case "U":
					return strings.ToUpper(val)
				case "L":
					return strings.ToLower(val)
				}
			}

			// ignore undefined operators
			inner = name
		}

		if val, ok := vars[inner]; ok {
			return val
		}
		return match
	})

	r := varsReplacer(vars)
	return r.Replace(template)
}
