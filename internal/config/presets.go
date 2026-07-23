package config

import (
	"fmt"
	"strings"

	"github.com/axispx/zeta/internal/models"
)

// Preset is an OpenAI-compatible provider template from the models catalog.
type Preset struct {
	ID           string
	Name         string
	BaseURL      string
	Models       map[string]ModelDef
	DefaultModel string // preferred Active when reconnecting with that model enabled
}

// PresetsFromModels converts catalog presets into config presets.
func PresetsFromModels(in []models.Preset) []Preset {
	out := make([]Preset, len(in))
	for i, p := range in {
		mds := make(map[string]ModelDef, len(p.Models))
		for id, m := range p.Models {
			mds[id] = ModelDef{Name: m.Name, ContextWindow: m.ContextWindow}
		}
		out[i] = Preset{
			ID:           p.ID,
			Name:         p.Name,
			BaseURL:      p.BaseURL,
			Models:       mds,
			DefaultModel: p.DefaultModel,
		}
	}
	return out
}

// ConnectPreset installs or updates a provider from a preset + API key.
// All catalog models are materialized; previously-enabled models stay enabled,
// and a brand-new provider starts with every model Disabled.
func (c *Config) ConnectPreset(pre Preset, apiKey string) error {
	if strings.TrimSpace(pre.ID) == "" {
		return fmt.Errorf("provider id required")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("api_key required")
	}
	if strings.TrimSpace(pre.BaseURL) == "" {
		return fmt.Errorf("provider %q: base_url required", pre.ID)
	}
	if len(pre.Models) == 0 {
		return fmt.Errorf("provider %q: models required", pre.ID)
	}

	var prev map[string]ModelDef
	if existing, ok := c.Providers[pre.ID]; ok {
		prev = existing.Models
	}
	models := MergeCatalogModels(pre.Models, prev)

	name := pre.Name
	if name == "" {
		name = pre.ID
	}
	if err := c.PutProvider(pre.ID, Provider{
		Name:    name,
		BaseURL: pre.BaseURL,
		APIKey:  apiKey,
		Models:  models,
		Custom:  false,
	}); err != nil {
		return err
	}

	if strings.TrimSpace(c.Active) != "" {
		return nil
	}
	if pre.DefaultModel != "" {
		if m, ok := models[pre.DefaultModel]; ok && m.Enabled() {
			c.Active = pre.ID + "/" + pre.DefaultModel
			return nil
		}
	}
	for _, id := range (Provider{Models: models}).ModelIDs() {
		if models[id].Enabled() {
			c.Active = pre.ID + "/" + id
			return nil
		}
	}
	return nil
}

// ConnectCustom installs a custom OpenAI-compatible provider (no models yet).
func (c *Config) ConnectCustom(id, name, baseURL, apiKey string) error {
	id = strings.TrimSpace(id)
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if id == "" {
		return fmt.Errorf("provider id required")
	}
	if _, ok := c.Provider(id); ok {
		return fmt.Errorf("%q is an existing provider id", id)
	}
	if baseURL == "" {
		return fmt.Errorf("base_url required")
	}
	if apiKey == "" {
		return fmt.Errorf("api_key required")
	}
	if strings.TrimSpace(name) == "" {
		name = id
	}
	return c.PutProvider(id, Provider{
		Name:    name,
		BaseURL: baseURL,
		APIKey:  apiKey,
		Models:  map[string]ModelDef{},
		Custom:  true,
	})
}
