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
	Active    string              `json:"active"` // provider/model eg: openai/gpt-5.6-luna
	Providers map[string]Provider `json:"providers"`
}

// Provider is an OpenAI-compatible API endpoint. The map key is the provider id.
type Provider struct {
	Name    string              `json:"name,omitempty"` // display label; defaults to map key
	BaseURL string              `json:"base_url"`
	APIKey  string              `json:"api_key"`
	Models  map[string]ModelDef `json:"models"`
	// Custom marks a user-defined endpoint (not from models.dev).
	Custom bool `json:"custom,omitempty"`
}

// ModelDef is one model entry under a provider. The map key is the model id.
type ModelDef struct {
	Name          string `json:"name,omitempty"` // display label; defaults to map key
	ContextWindow int    `json:"context_window"` // required; tokens the model can hold
	// Disabled keeps the model listed but excludes it from /model and Active.
	Disabled bool `json:"disabled,omitempty"`
}

// Enabled reports whether the model is selectable.
func (m ModelDef) Enabled() bool {
	return !m.Disabled
}

// DisplayName returns the model's display label.
func (m ModelDef) DisplayName(id string) string {
	if strings.TrimSpace(m.Name) != "" {
		return m.Name
	}
	return id
}

// DisplayName returns the provider's display label.
func (p Provider) DisplayName(id string) string {
	if strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	return id
}

// ProviderIDs returns sorted provider ids.
func (c Config) ProviderIDs() []string {
	ids := make([]string, 0, len(c.Providers))
	for id := range c.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
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

func choiceName(p Provider, providerID, modelID string) string {
	return fmt.Sprintf("%s %s", p.DisplayName(providerID), p.Models[modelID].DisplayName(modelID))
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

	cfg, err := parseConfig(data)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func parseConfig(data []byte) (Config, error) {
	var probe struct {
		Providers json.RawMessage `json:"providers"`
	}
	_ = json.Unmarshal(data, &probe)
	if len(probe.Providers) > 0 && probe.Providers[0] == '[' {
		return Config{}, fmt.Errorf("unsupported config: providers must be a map keyed by id, not an array")
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.Active = strings.TrimSpace(cfg.Active)
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

// Validate checks providers are complete and Active (if set) resolves.
// Empty config (no providers / no active) is allowed.
func (c Config) Validate() error {
	for id, p := range c.Providers {
		if err := validateProvider(id, p); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.Active) == "" {
		return nil
	}
	provider, model, err := ParseModelID(c.Active)
	if err != nil {
		return err
	}
	p, ok := c.Providers[provider]
	if !ok {
		return fmt.Errorf("model provider %q not found", provider)
	}
	if _, ok := p.Models[model]; !ok {
		return fmt.Errorf("model %q not in provider %q", model, provider)
	}
	if !p.Models[model].Enabled() {
		return fmt.Errorf("model %q is disabled", model)
	}
	return nil
}

func validateProvider(id string, p Provider) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("provider id required")
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return fmt.Errorf("provider %q: base_url required", id)
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return fmt.Errorf("provider %q: api_key required", id)
	}
	if len(p.Models) == 0 {
		return nil
	}
	for mid, m := range p.Models {
		if strings.TrimSpace(mid) == "" {
			return fmt.Errorf("provider %q: empty model id", id)
		}
		if m.ContextWindow <= 0 {
			return fmt.Errorf("provider %q: model %q: context_window required", id, mid)
		}
	}
	return nil
}

// Provider returns the provider with the given id.
func (c Config) Provider(id string) (Provider, bool) {
	p, ok := c.Providers[id]
	return p, ok
}

// ActiveChoice returns the configured provider+model pair, or false if unset/invalid.
func (c Config) ActiveChoice() (ModelChoice, bool) {
	provider, modelID, err := ParseModelID(c.Active)
	if err != nil {
		return ModelChoice{}, false
	}
	p, ok := c.Providers[provider]
	if !ok {
		return ModelChoice{}, false
	}
	if md, ok := p.Models[modelID]; !ok || !md.Enabled() {
		return ModelChoice{}, false
	}
	return ModelChoice{
		ProviderID: provider,
		ModelID:    modelID,
		Name:       choiceName(p, provider, modelID),
	}, true
}

// ActiveProvider returns the provider from cfg.Active.
func (c Config) ActiveProvider() (Provider, bool) {
	provider, _, err := ParseModelID(c.Active)
	if err != nil {
		return Provider{}, false
	}
	return c.Provider(provider)
}

// ActiveModelID returns the model id from cfg.Active.
func (c Config) ActiveModelID() string {
	_, model, err := ParseModelID(c.Active)
	if err != nil {
		return ""
	}
	return model
}

// ModelChoices returns all enabled provider+model pairs for the picker, sorted by name.
func (c Config) ModelChoices() []ModelChoice {
	var out []ModelChoice
	for _, pid := range c.ProviderIDs() {
		p := c.Providers[pid]
		for _, id := range p.ModelIDs() {
			if !p.Models[id].Enabled() {
				continue
			}
			out = append(out, ModelChoice{
				ProviderID: pid,
				ModelID:    id,
				Name:       choiceName(p, pid, id),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID() < out[j].ID()
	})
	return out
}

// ModelName is the active model display name shown in the footer.
func (c Config) ModelName() string {
	if ch, ok := c.ActiveChoice(); ok {
		return ch.Name
	}
	if strings.TrimSpace(c.Active) == "" {
		return "no config"
	}
	return c.Active
}

// ContextWindow returns the active model's context window in tokens, or 0.
func (c Config) ContextWindow() int {
	p, ok := c.ActiveProvider()
	id := c.ActiveModelID()
	if !ok || id == "" {
		return 0
	}
	m, ok := p.Models[id]
	if !ok {
		return 0
	}
	return m.ContextWindow
}

// SetActive sets the active model as provider_id/model_id.
func (c *Config) SetActive(id string) {
	c.Active = id
}
