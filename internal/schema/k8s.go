package schema

import (
	"regexp"
	"strings"
)

const DefaultK8sSchemaURL = "https://k8s-schemas.home-operations.com/" +
	"{group:-core}/{kindLower}_{versionLower}.json"

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
//	{groupSeg}      "<group>/" or ""
//	{groupFirst}    first DNS label of the group
//	{groupFirstSeg} "<groupFirst>-" or ""
//	{kind}          {kindLower}
//	{version}       {versionLower}
//
// Expression syntax (shell-like parameter expansion):
//
//	{var:-word}     "word" if var is empty, else var
//	{var:+word}     "word" if var is non-empty, else ""
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
		"kind":          strings.ToLower(kind),
		"kindLower":     strings.ToLower(kind),
		"version":       strings.ToLower(version),
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

		if val, ok := vars[inner]; ok {
			return val
		}
		return match
	})

	r := varsReplacer(vars)
	return r.Replace(template)
}
