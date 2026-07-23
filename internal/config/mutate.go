package config

import (
	"fmt"
	"strings"
)

// Clone returns a deep copy of the config.
func (c Config) Clone() Config {
	out := Config{Active: c.Active}
	if len(c.Providers) == 0 {
		return out
	}
	out.Providers = make(map[string]Provider, len(c.Providers))
	for id, p := range c.Providers {
		cp := Provider{
			Name:    p.Name,
			BaseURL: p.BaseURL,
			APIKey:  p.APIKey,
			Custom:  p.Custom,
		}
		if p.OAuth != nil {
			oc := *p.OAuth
			cp.OAuth = &oc
		}
		if len(p.Models) > 0 {
			cp.Models = make(map[string]ModelDef, len(p.Models))
			for mid, m := range p.Models {
				cp.Models[mid] = m
			}
		}
		out.Providers[id] = cp
	}
	return out
}

// PutProvider adds or replaces a provider by id, including its Models map.
func (c *Config) PutProvider(id string, p Provider) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("provider id required")
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return fmt.Errorf("provider %q: base_url required", id)
	}
	if !p.HasUsableCredential() {
		return fmt.Errorf("provider %q: api_key or oauth access_token required", id)
	}
	if p.Models == nil {
		p.Models = map[string]ModelDef{}
	}
	if c.Providers == nil {
		c.Providers = map[string]Provider{}
	}
	c.Providers[id] = p
	return nil
}

func (c *Config) withProvider(id string, fn func(*Provider) error) error {
	p, ok := c.Providers[id]
	if !ok {
		return fmt.Errorf("provider %q not found", id)
	}
	if err := fn(&p); err != nil {
		return err
	}
	c.Providers[id] = p
	return nil
}

// UpdateProvider updates display name / base URL / API key, preserving Models.
// Empty name/baseURL/apiKey arguments leave the existing value unchanged.
// Setting apiKey clears OAuth (credentials are mutually exclusive).
func (c *Config) UpdateProvider(id, name, baseURL, apiKey string) error {
	return c.withProvider(id, func(p *Provider) error {
		if name != "" {
			p.Name = name
		}
		if baseURL != "" {
			p.BaseURL = baseURL
		}
		if apiKey != "" {
			p.APIKey = apiKey
			p.OAuth = nil
		}
		if strings.TrimSpace(p.BaseURL) == "" {
			return fmt.Errorf("provider %q: base_url required", id)
		}
		if !p.HasUsableCredential() {
			return fmt.Errorf("provider %q: api_key or oauth access_token required", id)
		}
		return nil
	})
}

// DeleteProvider removes a provider by ID. If it owned the active model,
// Active switches to another available model or clears.
func (c *Config) DeleteProvider(id string) error {
	if _, ok := c.Providers[id]; !ok {
		return fmt.Errorf("provider %q not found", id)
	}
	delete(c.Providers, id)
	c.reselectActive()
	return nil
}

// UpsertModel adds or replaces a model under a provider. If cfg.Active is empty
// and this is the first model anywhere, it becomes the active model.
func (c *Config) UpsertModel(providerID, modelID string, m ModelDef) error {
	if strings.TrimSpace(providerID) == "" {
		return fmt.Errorf("provider id required")
	}
	if strings.TrimSpace(modelID) == "" {
		return fmt.Errorf("model id required")
	}
	if m.ContextWindow <= 0 {
		return fmt.Errorf("context_window required")
	}
	if err := c.withProvider(providerID, func(p *Provider) error {
		if p.Models == nil {
			p.Models = map[string]ModelDef{}
		}
		p.Models[modelID] = m
		return nil
	}); err != nil {
		return err
	}
	if strings.TrimSpace(c.Active) == "" && m.Enabled() {
		c.Active = providerID + "/" + modelID
	}
	return nil
}

// SetModelEnabled toggles a model's Disabled flag. Disabling the active model
// reselects another enabled model (or clears Active).
func (c *Config) SetModelEnabled(providerID, modelID string, enabled bool) error {
	if err := c.withProvider(providerID, func(p *Provider) error {
		md, ok := p.Models[modelID]
		if !ok {
			return fmt.Errorf("model %q not in provider %q", modelID, providerID)
		}
		md.Disabled = !enabled
		p.Models[modelID] = md
		return nil
	}); err != nil {
		return err
	}
	if !enabled {
		c.reselectActive()
	} else if strings.TrimSpace(c.Active) == "" {
		c.Active = providerID + "/" + modelID
	}
	return nil
}

// DeleteModel removes a model from a provider. The provider is kept even if it
// has no models left. If the model was active, Active is reselected or cleared.
func (c *Config) DeleteModel(providerID, modelID string) error {
	if err := c.withProvider(providerID, func(p *Provider) error {
		if _, ok := p.Models[modelID]; !ok {
			return fmt.Errorf("model %q not in provider %q", modelID, providerID)
		}
		delete(p.Models, modelID)
		return nil
	}); err != nil {
		return err
	}
	c.reselectActive()
	return nil
}

// MergeCatalogModels builds provider models from catalog defs. Existing Disabled
// flags and ReasoningEffort in prev are preserved; new catalog models arrive
// Disabled; ids absent from catalog are dropped. Invalid catalog entries
// (empty id / ctx<=0) skipped.
func MergeCatalogModels(catalog, prev map[string]ModelDef) map[string]ModelDef {
	next := make(map[string]ModelDef, len(catalog))
	for mid, def := range catalog {
		if strings.TrimSpace(mid) == "" || def.ContextWindow <= 0 {
			continue
		}
		def.Disabled = true
		if old, ok := prev[mid]; ok {
			def.Disabled = old.Disabled
			if strings.TrimSpace(old.ReasoningEffort) != "" {
				def.ReasoningEffort = old.ReasoningEffort
			}
		}
		next[mid] = def
	}
	return next
}

// SyncCatalogModels merges catalog defs into the provider. Existing Disabled
// flags are preserved; new catalog models arrive Disabled; models absent from
// the catalog are dropped.
func (c *Config) SyncCatalogModels(providerID string, catalog map[string]ModelDef) error {
	if err := c.withProvider(providerID, func(p *Provider) error {
		p.Models = MergeCatalogModels(catalog, p.Models)
		return nil
	}); err != nil {
		return err
	}
	c.reselectActive()
	return nil
}

// SetAllModelsEnabled enables or disables every model on a provider.
func (c *Config) SetAllModelsEnabled(providerID string, enabled bool) error {
	if err := c.withProvider(providerID, func(p *Provider) error {
		if len(p.Models) == 0 {
			return fmt.Errorf("no models to update")
		}
		for mid, md := range p.Models {
			md.Disabled = !enabled
			p.Models[mid] = md
		}
		return nil
	}); err != nil {
		return err
	}
	if !enabled {
		c.reselectActive()
	} else if strings.TrimSpace(c.Active) == "" {
		p, _ := c.Provider(providerID)
		for _, id := range p.ModelIDs() {
			c.Active = providerID + "/" + id
			break
		}
	}
	return nil
}

// reselectActive keeps Active if still valid; otherwise picks another model
// (same provider first) or clears Active when nothing remains.
func (c *Config) reselectActive() {
	if _, ok := c.ActiveChoice(); ok {
		return
	}
	prefer, _, _ := ParseModelID(c.Active)
	if prefer != "" {
		if p, ok := c.Provider(prefer); ok {
			for _, id := range p.ModelIDs() {
				if p.Models[id].Enabled() {
					c.Active = prefer + "/" + id
					return
				}
			}
		}
	}
	if ch := c.ModelChoices(); len(ch) > 0 {
		c.Active = ch[0].ID()
		return
	}
	c.Active = ""
}
