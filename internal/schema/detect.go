package schema

import (
	"github.com/goccy/go-yaml/ast"
)

type GVK struct {
	Group   string
	Version string
	Kind    string
}

// DetectGVK extracts apiVersion+kind from a document body. Returns ok=false
// when the document isn't a recognizable Kubernetes manifest. The read is a
// shallow top-level scan: goccy's NodeToValue decodes the entire subtree
// (every value, every list, every map) to satisfy a 2-field struct, so
// avoiding it on the per-keystroke hover/completion path is a meaningful
// win on large HelmRelease/Kustomization documents.
func DetectGVK(body ast.Node) (GVK, bool) {
	if body == nil {
		return GVK{}, false
	}
	apiVersion, kind, ok := topLevelStrings(body, "apiVersion", "kind")
	if !ok || apiVersion == "" || kind == "" {
		return GVK{}, false
	}
	group, version := splitOnce(apiVersion, '/')
	if version == "" {
		version = group
		group = ""
	}
	return GVK{Group: group, Version: version, Kind: kind}, true
}

// topLevelStrings reads apiVersion and kind from a top-level MappingNode
// without descending into subtrees. ok=false when the body isn't a
// mapping, or when either key is missing/non-scalar (a non-scalar
// apiVersion/kind is a malformed manifest, not a K8s doc to fetch a
// schema for).
func topLevelStrings(body ast.Node, a, b string) (string, string, bool) {
	m, ok := body.(*ast.MappingNode)
	if !ok || m == nil {
		return "", "", false
	}
	var av, kd string
	for _, mv := range m.Values {
		key, ok := mv.Key.(*ast.StringNode)
		if !ok {
			continue
		}
		switch key.Value {
		case a:
			if v, ok := mv.Value.(*ast.StringNode); ok {
				av = v.Value
			} else {
				return "", "", false
			}
		case b:
			if v, ok := mv.Value.(*ast.StringNode); ok {
				kd = v.Value
			} else {
				return "", "", false
			}
		}
	}
	return av, kd, true
}

func splitOnce(s string, sep byte) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}
