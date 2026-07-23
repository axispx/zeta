package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/axispx/zeta/internal/paths"
)

const (
	defaultAPIURL = "https://models.dev/api.json"
	cacheFile     = "models.json"
	cacheTTL      = 5 * time.Minute
	httpTimeout   = 15 * time.Second
	userAgent     = "zeta"
	minContext    = 8_000 // skip tiny/image-only models
)

func apiEndpoint() string {
	if u := strings.TrimSpace(os.Getenv("ZETA_MODELS_URL")); u != "" {
		return u
	}
	return defaultAPIURL
}

// Provider is one models.dev provider entry (fields we use).
type Provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	API    string           `json:"api"`
	NPM    string           `json:"npm"`
	Env    []string         `json:"env"`
	Models map[string]Model `json:"models"`
}

// Model is one models.dev model entry (fields we use).
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	ToolCall bool   `json:"tool_call"`
	Limit    Limit  `json:"limit"`
}

// Limit is token limits for a model.
type Limit struct {
	Context int `json:"context"`
}

// ModelInfo is a catalog model reduced for OpenAI-compatible presets.
type ModelInfo struct {
	Name          string
	ContextWindow int
}

// Preset is an OpenAI-compatible provider template from the catalog.
// Convert via config.PresetsFromModels at the config boundary.
type Preset struct {
	ID           string
	Name         string
	BaseURL      string
	Models       map[string]ModelInfo
	DefaultModel string // preferred Active when reconnecting with that model enabled
}

// CachePath returns $ZETA_HOME/cache/models.json.
func CachePath() string {
	home := paths.Home()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "cache", cacheFile)
}

// Load returns OpenAI-compatible presets from cache and/or models.dev.
// Fresh cache (<5m) is used as-is; otherwise a network refresh is attempted.
// On fetch failure the existing cache is kept and returned (never deleted).
func Load() ([]Preset, error) {
	path := CachePath()
	cached, mtime, cacheErr := readCache(path)
	if cacheErr == nil && time.Since(mtime) < cacheTTL {
		return presetsFromCatalog(cached), nil
	}

	fresh, fetchErr := fetch()
	if fetchErr == nil {
		_ = writeCache(path, fresh)
		return presetsFromCatalog(fresh), nil
	}

	if cacheErr == nil {
		return presetsFromCatalog(cached), fetchErr
	}
	return nil, fetchErr
}

func fetch() (map[string]Provider, error) {
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest(http.MethodGet, apiEndpoint(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev: %s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var catalog map[string]Provider
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, fmt.Errorf("models.dev: parse: %w", err)
	}
	for id, p := range catalog {
		p.ID = id
		catalog[id] = p
	}
	return catalog, nil
}

func readCache(path string) (map[string]Provider, time.Time, error) {
	if path == "" {
		return nil, time.Time{}, fmt.Errorf("no cache path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var catalog map[string]Provider
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, time.Time{}, err
	}
	for id, p := range catalog {
		p.ID = id
		catalog[id] = p
	}
	return catalog, info.ModTime(), nil
}

func writeCache(path string, catalog map[string]Provider) error {
	if path == "" {
		return fmt.Errorf("no cache path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func presetsFromCatalog(catalog map[string]Provider) []Preset {
	var out []Preset
	for _, p := range catalog {
		if !openAICompatible(p) {
			continue
		}
		pre, ok := toPreset(p)
		if !ok {
			continue
		}
		out = append(out, pre)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// openAICompatible reports whether models.dev tags this provider with an
// OpenAI Chat Completions–compatible SDK. Non-OpenAI protocols (Anthropic,
// Google, Bedrock, …) are excluded until zeta has native clients for them.
func openAICompatible(p Provider) bool {
	switch p.NPM {
	case "@ai-sdk/openai-compatible",
		"@ai-sdk/openai",
		"@ai-sdk/azure",
		"@ai-sdk/cerebras",
		"@ai-sdk/deepinfra",
		"@ai-sdk/gateway",
		"@ai-sdk/groq",
		"@ai-sdk/perplexity",
		"@ai-sdk/togetherai",
		"@ai-sdk/vercel",
		"@ai-sdk/xai",
		"@aihubmix/ai-sdk-provider",
		"@openrouter/ai-sdk-provider",
		"ai-gateway-provider",
		"merge-gateway-ai-sdk-provider",
		"venice-ai-sdk-provider":
		return true
	}
	return strings.Contains(p.NPM, "openai-compatible")
}

func toPreset(p Provider) (Preset, bool) {
	base := strings.TrimRight(strings.TrimSpace(p.API), "/")
	if base == "" {
		return Preset{}, false
	}
	models := map[string]ModelInfo{}
	var defaultModel string
	ids := make([]string, 0, len(p.Models))
	for id := range p.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := p.Models[id]
		if m.Status == "deprecated" {
			continue
		}
		ctx := m.Limit.Context
		if ctx < minContext {
			continue
		}
		name := m.Name
		if name == "" {
			name = id
		}
		models[id] = ModelInfo{Name: name, ContextWindow: ctx}
		if defaultModel == "" {
			defaultModel = id
		}
	}
	if len(models) == 0 {
		return Preset{}, false
	}
	name := p.Name
	if name == "" {
		name = p.ID
	}
	return Preset{
		ID:           p.ID,
		Name:         name,
		BaseURL:      base,
		Models:       models,
		DefaultModel: defaultModel,
	}, true
}
