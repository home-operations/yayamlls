package config

import "encoding/json"

type Settings struct {
	Schemas           map[string][]string        `json:"schemas,omitempty"`
	Catalog           *bool                      `json:"catalog,omitempty"`
	CatalogURL        string                     `json:"catalogUrl,omitempty"`
	Kubernetes        *KubernetesSettings        `json:"kubernetes,omitempty"`
	Renderers         map[string]json.RawMessage `json:"renderers,omitempty"`
	FluxSubstitutions *bool                      `json:"fluxSubstitutions,omitempty"`
	// RenderDebounceMs is the debounce, in milliseconds, before a document
	// change triggers a renderer (e.g. flate). Unset or non-positive uses the
	// built-in default.
	RenderDebounceMs *int `json:"renderDebounceMs,omitempty"`
	// RenderTimeoutMs caps how long a single render may run, in milliseconds,
	// before its context deadline trips. Unset or non-positive uses the
	// built-in default.
	RenderTimeoutMs *int `json:"renderTimeoutMs,omitempty"`
	// CustomTags lists YAML tags (e.g. "!Ref", "!vault scalar") whose values
	// an external tool resolves; nodes carrying them skip schema validation.
	CustomTags []string `json:"customTags,omitempty"`
}

type KubernetesSettings struct {
	// Enabled gates apiVersion+kind detection and Flux rendering. nil (unset)
	// is enabled; set false to run as a generic YAML language server.
	Enabled *bool `json:"enabled,omitempty"`
	// SchemaURL templates per-document apiVersion+kind auto-detect.
	// See schema.BuildK8sURL for supported placeholders.
	SchemaURL string `json:"schemaUrl,omitempty"`
}

// CatalogEnabled treats nil (unset) as enabled.
func (s *Settings) CatalogEnabled() bool {
	if s == nil || s.Catalog == nil {
		return true
	}
	return *s.Catalog
}

func (s *Settings) FluxSubstitutionsEnabled() bool {
	if s == nil || s.FluxSubstitutions == nil {
		return false
	}
	return *s.FluxSubstitutions
}

// CustomTagNames returns the configured custom YAML tags, or nil.
func (s *Settings) CustomTagNames() []string {
	if s == nil {
		return nil
	}
	return s.CustomTags
}

// KubernetesEnabled treats nil (unset) as enabled.
func (s *Settings) KubernetesEnabled() bool {
	if s == nil || s.Kubernetes == nil || s.Kubernetes.Enabled == nil {
		return true
	}
	return *s.Kubernetes.Enabled
}

func Parse(raw json.RawMessage) (Settings, error) {
	var s Settings
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, err
	}
	return s, nil
}
