package yamlast

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/goccy/go-yaml"
)

// yaml11Words are strings YAML 1.1 parsers (still common in tooling) resolve
// to booleans or null even though the 1.2 core schema keeps them as strings.
var yaml11Words = map[string]bool{
	"y": true, "n": true, "yes": true, "no": true, "on": true, "off": true,
	"true": true, "false": true, "null": true, "~": true,
}

// coreNumber is the YAML 1.2 core-schema number grammar; goccy's decoder is
// laxer than the spec (it leaves `1e3` a string), so check it explicitly.
var coreNumber = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)

// QuoteString renders s as a YAML scalar that reads back as the same string:
// plain when that is unambiguous, otherwise double-quoted.
func QuoteString(s string) string {
	if plainSafe(s) {
		return s
	}
	return strconv.Quote(s)
}

func plainSafe(s string) bool {
	if s == "" || yaml11Words[strings.ToLower(s)] || coreNumber.MatchString(s) {
		return false
	}
	if strings.ContainsAny(s[:1], "-?:,[]{}#&*!|>'\"%@`") || strings.ContainsAny(s, "\n\r\t") {
		return false
	}
	if strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) }) ||
		s[0] == ' ' || s[len(s)-1] == ' ' ||
		strings.Contains(s, ": ") || strings.Contains(s, " #") || strings.HasSuffix(s, ":") {
		return false
	}
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err != nil {
		return false
	}
	got, ok := v.(string)
	return ok && got == s
}

// FormatValue renders a decoded JSON-schema value (enum member, default) as
// YAML text: strings via QuoteString, everything else as compact JSON, which
// is valid YAML flow syntax.
func FormatValue(v any) string {
	if s, ok := v.(string); ok {
		return QuoteString(s)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
