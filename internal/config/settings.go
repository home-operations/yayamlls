package config

import "encoding/json"

type Settings struct {
	Schemas           map[string][]string        `json:"schemas,omitempty"`
	Catalog           *bool                      `json:"catalog,omitempty"`
	CatalogURL        string                     `json:"catalogUrl,omitempty"`
	Kubernetes        *KubernetesSettings        `json:"kubernetes,omitempty"`
	Renderers         map[string]json.RawMessage `json:"renderers,omitempty"`
	FluxSubstitutions *bool                      `json:"fluxSubstitutions,omitempty"`
}

type KubernetesSettings struct {
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
