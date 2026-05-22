// Package links produces textDocument/documentLink results. v1 only
// recognises the `# yaml-language-server: $schema=URL` modeline and
// turns the URL into a clickable link.
package links

import (
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

const modelinePrefix = "# yaml-language-server:"

func Links(text string) []protocol.DocumentLink {
	var out []protocol.DocumentLink
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, modelinePrefix) {
			continue
		}
		rest := trimmed[len(modelinePrefix):]
		for _, kv := range strings.Fields(rest) {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || strings.TrimSpace(k) != "$schema" {
				continue
			}
			url := strings.TrimSpace(v)
			if !isHTTPURL(url) {
				continue
			}
			startCol := strings.Index(line, url)
			if startCol < 0 {
				continue
			}
			target := url
			out = append(out, protocol.DocumentLink{
				Range: protocol.Range{
					Start: protocol.Position{Line: uint32(i), Character: uint32(startCol)},
					End:   protocol.Position{Line: uint32(i), Character: uint32(startCol + len(url))},
				},
				Target: &target,
			})
		}
	}
	return out
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
