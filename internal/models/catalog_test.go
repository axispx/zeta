package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFromFreshCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)

	catalog := map[string]Provider{
		"deepseek": {
			ID:   "deepseek",
			Name: "DeepSeek",
			API:  "https://api.deepseek.com",
			NPM:  "@ai-sdk/openai-compatible",
			Env:  []string{"DEEPSEEK_API_KEY"},
			Models: map[string]Model{
				"deepseek-v4-flash": {
					Name:     "V4 Flash",
					ToolCall: true,
					Limit:    Limit{Context: 1_000_000},
				},
			},
		},
		"anthropic": {
			ID:   "anthropic",
			Name: "Anthropic",
			NPM:  "@ai-sdk/anthropic",
			Models: map[string]Model{
				"claude": {Name: "Claude", Limit: Limit{Context: 200_000}},
			},
		},
	}
	path := CachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(catalog)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	presets, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 1 || presets[0].ID != "deepseek" {
		t.Fatalf("presets = %#v", presets)
	}
	if presets[0].BaseURL != "https://api.deepseek.com" {
		t.Fatalf("BaseURL = %q", presets[0].BaseURL)
	}
	if presets[0].Models["deepseek-v4-flash"].ContextWindow != 1_000_000 {
		t.Fatalf("context = %#v", presets[0].Models)
	}
}

func TestLoadFallsBackToStaleCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)
	t.Setenv("ZETA_MODELS_URL", "http://127.0.0.1:1/api.json") // force fetch failure

	path := CachePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	stale := map[string]Provider{
		"xai": {
			ID:   "xai",
			Name: "xAI",
			API:  "https://api.x.ai/v1",
			NPM:  "@ai-sdk/xai",
			Models: map[string]Model{
				"grok-3": {Name: "Grok 3", Limit: Limit{Context: 131_072}},
			},
		},
	}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	presets, err := Load()
	if err == nil {
		t.Fatal("expected fetch error with stale fallback")
	}
	if len(presets) != 1 || presets[0].ID != "xai" {
		t.Fatalf("presets = %#v", presets)
	}
	// Failed fetch must not wipe the on-disk cache.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache should remain: %v", err)
	}
}

func TestLoadNoCacheNoNetwork(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)
	t.Setenv("ZETA_MODELS_URL", "http://127.0.0.1:1/api.json")

	presets, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if len(presets) != 0 {
		t.Fatalf("expected no builtin fallback, got %#v", presets)
	}
}

func TestNormalizeAndFilter(t *testing.T) {
	cat := map[string]Provider{
		"deepseek": {
			ID: "deepseek", Name: "DeepSeek", API: "https://api.deepseek.com",
			NPM: "@ai-sdk/openai-compatible",
			Models: map[string]Model{
				"ok":   {Name: "OK", Limit: Limit{Context: 100_000}},
				"tiny": {Name: "Tiny", Limit: Limit{Context: 100}},
				"old":  {Name: "Old", Status: "deprecated", Limit: Limit{Context: 100_000}},
			},
		},
		// Anthropic Messages — not OpenAI-compatible even if api looks versioned.
		"anthropic": {
			ID: "anthropic", Name: "Anthropic", API: "https://api.anthropic.com/v1",
			NPM: "@ai-sdk/anthropic",
			Models: map[string]Model{"c": {Name: "C", Limit: Limit{Context: 200_000}}},
		},
		"google": {
			ID: "google", Name: "Google", NPM: "@ai-sdk/google",
			Models: map[string]Model{"g": {Name: "G", Limit: Limit{Context: 100_000}}},
		},
		"openrouter": {
			ID: "openrouter", Name: "OpenRouter", API: "https://openrouter.ai/api/v1",
			NPM: "@openrouter/ai-sdk-provider",
			Models: map[string]Model{"m": {Name: "M", Limit: Limit{Context: 100_000}}},
		},
	}
	presets := presetsFromCatalog(cat)
	if len(presets) != 2 {
		t.Fatalf("got %d %#v", len(presets), presets)
	}
	ids := map[string]bool{}
	for _, p := range presets {
		ids[p.ID] = true
	}
	if !ids["deepseek"] || !ids["openrouter"] {
		t.Fatalf("ids = %#v", ids)
	}
	for _, p := range presets {
		if p.ID != "deepseek" {
			continue
		}
		if _, ok := p.Models["ok"]; !ok {
			t.Fatal("missing ok")
		}
		if _, ok := p.Models["tiny"]; ok {
			t.Fatal("tiny should be filtered")
		}
		if _, ok := p.Models["old"]; ok {
			t.Fatal("deprecated should be filtered")
		}
	}
}

func TestBaseURLRequiresAPI(t *testing.T) {
	cat := map[string]Provider{
		"openai": {
			// models.dev leaves api empty for first-party SDKs — skip until set
			ID: "openai", Name: "OpenAI", NPM: "@ai-sdk/openai",
			Models: map[string]Model{"m": {Name: "M", Limit: Limit{Context: 100_000}}},
		},
		"deepseek": {
			ID: "deepseek", Name: "DeepSeek", API: "https://api.deepseek.com",
			NPM: "@ai-sdk/openai-compatible",
			Models: map[string]Model{"m": {Name: "M", Limit: Limit{Context: 100_000}}},
		},
	}
	presets := presetsFromCatalog(cat)
	if len(presets) != 1 || presets[0].ID != "deepseek" {
		t.Fatalf("presets = %#v", presets)
	}
	if presets[0].BaseURL != "https://api.deepseek.com" {
		t.Fatalf("BaseURL = %q", presets[0].BaseURL)
	}
}

func TestPresetsSortedByName(t *testing.T) {
	cat := map[string]Provider{
		"togetherai": {
			ID: "togetherai", Name: "Together AI", API: "https://api.together.xyz/v1",
			NPM: "@ai-sdk/togetherai",
			Models: map[string]Model{"m": {Name: "M", Limit: Limit{Context: 100_000}}},
		},
		"openai": {
			ID: "openai", Name: "OpenAI", API: "https://api.openai.com/v1", NPM: "@ai-sdk/openai",
			Models: map[string]Model{"m": {Name: "M", Limit: Limit{Context: 100_000}}},
		},
		"deepseek": {
			ID: "deepseek", Name: "DeepSeek", API: "https://api.deepseek.com",
			NPM: "@ai-sdk/openai-compatible",
			Models: map[string]Model{"m": {Name: "M", Limit: Limit{Context: 100_000}}},
		},
	}
	presets := presetsFromCatalog(cat)
	if len(presets) != 3 {
		t.Fatalf("got %d", len(presets))
	}
	want := []string{"DeepSeek", "OpenAI", "Together AI"}
	for i, name := range want {
		if presets[i].Name != name {
			t.Fatalf("presets[%d] = %q, want %q", i, presets[i].Name, name)
		}
	}
}
