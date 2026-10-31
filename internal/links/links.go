// Package links handles textDocument/documentLink for the modeline $schema URL.
package links

import (
	"strings"

	"github.com/home-operations/yayamlls/internal/yamlast"
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
		restStart := len(line) - len(trimmed) + len(modelinePrefix)
		cursor := restStart
		for kv := range strings.FieldsSeq(line[restStart:]) {
			cursor += strings.Index(line[cursor:], kv)
			kvStart := cursor
			cursor += len(kv)
			k, v, ok := strings.Cut(kv, "=")
			if !ok || strings.TrimSpace(k) != "$schema" || !isHTTPURL(v) {
				continue
			}
			startCol := yamlast.UTF16Len(line[:kvStart+len(k)+1])
			target := v
			out = append(out, protocol.DocumentLink{
				Range: protocol.Range{
					Start: protocol.Position{Line: uint32(i), Character: startCol},
					End:   protocol.Position{Line: uint32(i), Character: startCol + yamlast.UTF16Len(v)},
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
