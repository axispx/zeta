package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/axispx/zeta/internal/paths"
)

// Config is zeta's provider configuration.
type Config struct {
	Model     string     `json:"model"` // provider_id/model_id
	Providers []Provider `json:"providers"`
}

// Provider is an OpenAI-compatible API endpoint.
type Provider struct {
	ID      string              `json:"id"`
	Name    string              `json:"name,omitempty"` // display label; defaults to id
	BaseURL string              `json:"base_url"`
	APIKey  string              `json:"api_key"`
	Models  map[string]ModelDef `json:"models"`
}

// ModelDef is one model entry under a provider. The map key is the model id.
type ModelDef struct {
	Name string `json:"name,omitempty"` // display label; defaults to map key
}

// DisplayName returns the model's display label.
func (m ModelDef) DisplayName(id string) string {
	if strings.TrimSpace(m.Name) != "" {
		return m.Name
	}
	return id
}

// DisplayName returns the provider's display label.
func (p Provider) DisplayName() string {
	if strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	return p.ID
}

// ModelIDs returns sorted model ids for this provider.
func (p Provider) ModelIDs() []string {
	ids := make([]string, 0, len(p.Models))
	for id := range p.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ModelChoice is one selectable provider+model pair.
type ModelChoice struct {
	ProviderID string
	ModelID    string
	Name       string // display: "DeepSeek V4 Flash"
}

// ID returns the provider_id/model_id.
func (c ModelChoice) ID() string {
	return c.ProviderID + "/" + c.ModelID
}

func choiceName(p Provider, modelID string) string {
	return fmt.Sprintf("%s %s", p.DisplayName(), p.Models[modelID].DisplayName(modelID))
}

// Path returns $ZETA_HOME/config.json, or ~/.zeta/config.json.
func Path() string {
	d := Dir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "config.json")
}

// Dir returns the zeta home directory ($ZETA_HOME or ~/.zeta).
func Dir() string {
	return paths.Home()
}

// Load reads and parses the config file. Missing file returns empty Config + nil error.
// A present file is validated; invalid contents return an error.
func Load() (Config, error) {
	path := Path()
	if path == "" {
		return Config{}, fmt.Errorf("cannot resolve config path")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes the config to disk.
func (c Config) Save() error {
	if err := c.Validate(); err != nil {
		return err
	}
	path := Path()
	if path == "" {
		return fmt.Errorf("cannot resolve config path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// ParseModelID splits provider_id/model_id on the first slash.
func ParseModelID(id string) (provider, model string, err error) {
	i := strings.Index(id, "/")
	if i <= 0 || i >= len(id)-1 {
		return "", "", fmt.Errorf("model must be provider/model")
	}
	return id[:i], id[i+1:], nil
}

// Validate checks config is complete and model references a configured provider.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("model not set")
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("no providers configured")
	}
	for i := range c.Providers {
		if err := validateProvider(&c.Providers[i]); err != nil {
			return err
		}
	}
	provider, model, err := ParseModelID(c.Model)
	if err != nil {
		return err
	}
	p := c.Provider(provider)
	if p == nil {
		return fmt.Errorf("model provider %q not found", provider)
	}
	if _, ok := p.Models[model]; !ok {
		return fmt.Errorf("model %q not in provider %q", model, provider)
	}
	return nil
}

func validateProvider(p *Provider) error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("provider id required")
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return fmt.Errorf("provider %q: base_url required", p.ID)
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return fmt.Errorf("provider %q: api_key required", p.ID)
	}
	if len(p.Models) == 0 {
		return fmt.Errorf("provider %q: models required", p.ID)
	}
	for id := range p.Models {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("provider %q: empty model id", p.ID)
		}
	}
	return nil
}

// Provider returns the provider with the given id, or nil.
func (c Config) Provider(id string) *Provider {
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			return &c.Providers[i]
		}
	}
	return nil
}

// ActiveChoice returns the configured provider+model pair, or false if unset/invalid.
func (c Config) ActiveChoice() (ModelChoice, bool) {
	provider, modelID, err := ParseModelID(c.Model)
	if err != nil {
		return ModelChoice{}, false
	}
	p := c.Provider(provider)
	if p == nil {
		return ModelChoice{}, false
	}
	if _, ok := p.Models[modelID]; !ok {
		return ModelChoice{}, false
	}
	return ModelChoice{
		ProviderID: provider,
		ModelID:    modelID,
		Name:       choiceName(*p, modelID),
	}, true
}

// ActiveProvider returns the provider from cfg.Model, or nil.
func (c Config) ActiveProvider() *Provider {
	provider, _, err := ParseModelID(c.Model)
	if err != nil {
		return nil
	}
	return c.Provider(provider)
}

// ActiveModelID returns the model id from cfg.Model.
func (c Config) ActiveModelID() string {
	_, model, err := ParseModelID(c.Model)
	if err != nil {
		return ""
	}
	return model
}

// ModelChoices returns all provider+model pairs for the picker.
func (c Config) ModelChoices() []ModelChoice {
	var out []ModelChoice
	for _, p := range c.Providers {
		for _, id := range p.ModelIDs() {
			out = append(out, ModelChoice{
				ProviderID: p.ID,
				ModelID:    id,
				Name:       choiceName(p, id),
			})
		}
	}
	return out
}

// ModelName is the active model display name shown in the footer.
func (c Config) ModelName() string {
	if ch, ok := c.ActiveChoice(); ok {
		return ch.Name
	}
	if strings.TrimSpace(c.Model) == "" {
		return "no config"
	}
	return c.Model
}

// SetModel sets the active model as provider_id/model_id.
func (c *Config) SetModel(id string) {
	c.Model = id
}
