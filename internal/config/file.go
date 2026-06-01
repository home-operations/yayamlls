package config

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"

	yaml "github.com/goccy/go-yaml"
	"github.com/home-operations/yayamlls/internal/uri"
)

const WorkspaceConfigFile = ".yayamlls.yaml"

const WorkspaceConfigFileFallback = ".yamlls.yaml"

// LoadFromWorkspace reads `.yayamlls.yaml` (or `.yamlls.yaml`) from the
// workspace root. Relative schema paths are anchored at the workspace root.
func LoadFromWorkspace(rootURI string) (Settings, error) {
	root := workspacePath(rootURI)
	if root == "" {
		return Settings{}, nil
	}
	for _, name := range []string{WorkspaceConfigFile, WorkspaceConfigFileFallback} {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		s, err := LoadFile(path)
		if err != nil {
			return s, err
		}
		expandSchemaPaths(&s, root)
		return s, nil
	}
	return Settings{}, nil
}

func expandSchemaPaths(s *Settings, root string) {
	if len(s.Schemas) == 0 {
		return
	}
	expanded := make(map[string][]string, len(s.Schemas))
	for ref, globs := range s.Schemas {
		expanded[expandRef(ref, root)] = globs
	}
	s.Schemas = expanded
}

func expandRef(ref, root string) string {
	if ref == "" || filepath.IsAbs(ref) {
		return ref
	}
	for _, prefix := range []string{"http://", "https://", "file://"} {
		if strings.HasPrefix(ref, prefix) {
			return ref
		}
	}
	return filepath.Clean(filepath.Join(root, ref))
}

// LoadFile reads a YAML config file. A missing file returns Settings{}
// and nil; callers should not treat absence as an error.
func LoadFile(path string) (Settings, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	jsonBytes, err := yaml.YAMLToJSON(b)
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(jsonBytes, &s); err != nil {
		return Settings{}, err
	}
	return s, nil
}

func workspacePath(rootURI string) string {
	if rootURI == "" {
		return ""
	}
	return uri.ToPath(rootURI)
}

// Merge applies override on top of base. Non-zero scalars and non-nil
// maps in override win; map keys union.
func Merge(base, override Settings) Settings {
	out := base
	if override.Schemas != nil {
		merged := make(map[string][]string, len(out.Schemas)+len(override.Schemas))
		maps.Copy(merged, out.Schemas)
		for key, globs := range override.Schemas {
			merged[key] = unionStrings(merged[key], globs)
		}
		out.Schemas = merged
	}
	if override.Catalog != nil {
		out.Catalog = override.Catalog
	}
	if override.CatalogURL != "" {
		out.CatalogURL = override.CatalogURL
	}
	if override.Kubernetes != nil {
		out.Kubernetes = override.Kubernetes
	}
	if override.Renderers != nil {
		if out.Renderers == nil {
			out.Renderers = make(map[string]json.RawMessage)
		}
		maps.Copy(out.Renderers, override.Renderers)
	}
	if override.FluxSubstitutions != nil {
		out.FluxSubstitutions = override.FluxSubstitutions
	}
	if override.CustomTags != nil {
		out.CustomTags = unionStrings(out.CustomTags, override.CustomTags)
	}
	return out
}

// unionStrings appends override values to base, dropping duplicates and
// preserving base order. Used for schema globs and custom tags: a colliding
// key or a second config layer should widen the set, not replace it.
func unionStrings(base, override []string) []string {
	seen := make(map[string]bool, len(base)+len(override))
	out := make([]string, 0, len(base)+len(override))
	for _, g := range append(append([]string(nil), base...), override...) {
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	return out
}
