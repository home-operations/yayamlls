package schema

import (
	"bytes"
	_ "embed"
	"errors"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed embedded/yayamlls.schema.json
var yayamllsSchemaJSON []byte

const EmbeddedYamllsSchemaURL = "embedded://yayamlls/yayamlls.schema.json"

// embeddedLoader is a jsonschema.URLLoader serving the built-in yayamlls
// config schema for embedded:// URLs.
type embeddedLoader struct{}

func (embeddedLoader) Load(url string) (any, error) {
	if url == EmbeddedYamllsSchemaURL {
		return jsonschema.UnmarshalJSON(bytes.NewReader(yayamllsSchemaJSON))
	}
	return nil, errors.New("unknown embedded resource: " + url)
}

func isYamllsConfigPath(docPath string) bool {
	if docPath == "" {
		return false
	}
	if i := strings.LastIndex(docPath, "/"); i >= 0 {
		docPath = docPath[i+1:]
	}
	return docPath == ".yayamlls.yaml" || docPath == ".yayamlls.yml" ||
		docPath == ".yamlls.yaml" || docPath == ".yamlls.yml"
}
