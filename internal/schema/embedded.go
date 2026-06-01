package schema

import (
	"bytes"
	_ "embed"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed embedded/yayamlls.schema.json
var yayamllsSchemaJSON []byte

const EmbeddedYamllsSchemaURL = "embedded://yayamlls/yayamlls.schema.json"

var installEmbeddedOnce sync.Once

func installEmbeddedLoader() {
	installEmbeddedOnce.Do(func() {
		jsonschema.Loaders["embedded"] = func(url string) (io.ReadCloser, error) {
			if url == EmbeddedYamllsSchemaURL {
				return io.NopCloser(bytes.NewReader(yayamllsSchemaJSON)), nil
			}
			return nil, errors.New("unknown embedded resource: " + url)
		}
	})
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
