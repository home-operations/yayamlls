package schema

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Resolve walks root to the subschema at the JSON Pointer ptr, unwrapping
// $ref chains and composition branches (allOf/anyOf/oneOf).
func Resolve(root *jsonschema.Schema, ptr string) *jsonschema.Schema {
	if root == nil {
		return nil
	}
	cur := follow(root)
	if ptr == "" || ptr == "/" {
		return cur
	}
	for _, seg := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		next := step(cur, unescape(seg))
		if next == nil {
			return nil
		}
		cur = follow(next)
	}
	return cur
}

func unescape(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
}

// follow unwraps a $ref chain, but stops at a node that carries its own
// keywords: under draft 2020-12 a $ref and its sibling keywords merge, so
// jumping straight to the ref target would discard the siblings.
func follow(s *jsonschema.Schema) *jsonschema.Schema {
	for i := 0; s != nil && i < 32; i++ {
		ref := directRef(s)
		if ref == nil || carriesOwnKeywords(s) {
			return s
		}
		s = ref
	}
	return s
}

func directRef(s *jsonschema.Schema) *jsonschema.Schema {
	switch {
	case s.Ref != nil:
		return s.Ref
	case s.RecursiveRef != nil:
		return s.RecursiveRef
	case s.DynamicRef != nil && s.DynamicRef.Ref != nil:
		return s.DynamicRef.Ref
	}
	return nil
}

func carriesOwnKeywords(s *jsonschema.Schema) bool {
	return len(s.Properties) > 0 || len(s.PatternProperties) > 0 ||
		s.AdditionalProperties != nil || len(s.AllOf) > 0 ||
		len(s.AnyOf) > 0 || len(s.OneOf) > 0 || s.Enum != nil ||
		s.Const != nil || s.Default != nil || s.Types != nil ||
		len(s.Required) > 0 || s.Description != "" ||
		len(s.PrefixItems) > 0 || s.Items2020 != nil || s.Items != nil
}

// mergeSources returns the sub-schemas whose keywords apply alongside s: its
// $ref target and composition branches. Walking these instead of following a
// single ref is what merges a $ref with its sibling keywords.
func mergeSources(s *jsonschema.Schema) []*jsonschema.Schema {
	out := make([]*jsonschema.Schema, 0, 4)
	if ref := directRef(s); ref != nil {
		out = append(out, ref)
	}
	out = append(out, s.AllOf...)
	out = append(out, s.AnyOf...)
	out = append(out, s.OneOf...)
	return out
}

func step(s *jsonschema.Schema, seg string) *jsonschema.Schema {
	return stepDepth(s, seg, 0)
}

func stepDepth(s *jsonschema.Schema, seg string, depth int) *jsonschema.Schema {
	if s == nil || depth > 32 {
		return nil
	}
	if isNumber(seg) {
		idx, _ := strconv.Atoi(seg)
		if idx < len(s.PrefixItems) {
			return s.PrefixItems[idx]
		}
		if s.Items2020 != nil {
			return s.Items2020
		}
		switch it := s.Items.(type) {
		case *jsonschema.Schema:
			return it
		case []*jsonschema.Schema:
			if idx < len(it) {
				return it[idx]
			}
		}
	} else if p, ok := s.Properties[seg]; ok {
		return p
	}
	for _, src := range mergeSources(s) {
		if r := stepDepth(src, seg, depth+1); r != nil {
			return r
		}
	}
	if isNumber(seg) {
		return nil
	}
	// patternProperties before additionalProperties, matching JSON Schema
	// precedence; common for label/annotation maps and `x-` extension keys.
	for re, sub := range s.PatternProperties {
		if re.MatchString(seg) {
			return sub
		}
	}
	if ap, ok := s.AdditionalProperties.(*jsonschema.Schema); ok {
		return ap
	}
	return nil
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Properties returns the merged property map across allOf/anyOf/oneOf
// branches. First definition wins on key collision.
func Properties(s *jsonschema.Schema) map[string]*jsonschema.Schema {
	out := make(map[string]*jsonschema.Schema)
	collectProperties(follow(s), out, 0)
	return out
}

// Enums returns the candidate scalar values for a node: enum members and a
// const, gathered across allOf/anyOf/oneOf branches so a value constrained
// inside a composition branch or $ref still surfaces. Order is preserved and
// duplicates are dropped. Returns nil when the schema admits no fixed value.
func Enums(s *jsonschema.Schema) []any {
	var out []any
	seen := make(map[string]bool)
	add := func(v any) {
		key := fmt.Sprintf("%v", v)
		if !seen[key] {
			seen[key] = true
			out = append(out, v)
		}
	}
	collectEnums(follow(s), add, 0)
	return out
}

func collectEnums(s *jsonschema.Schema, add func(any), depth int) {
	if s == nil || depth > 16 {
		return
	}
	if s.Enum != nil {
		for _, v := range s.Enum.Values {
			add(v)
		}
	}
	if s.Const != nil {
		add(*s.Const)
	}
	for _, src := range mergeSources(s) {
		collectEnums(src, add, depth+1)
	}
}

func collectProperties(s *jsonschema.Schema, into map[string]*jsonschema.Schema, depth int) {
	if s == nil || depth > 16 {
		return
	}
	for k, v := range s.Properties {
		if _, exists := into[k]; !exists {
			into[k] = v
		}
	}
	for _, src := range mergeSources(s) {
		collectProperties(src, into, depth+1)
	}
}
