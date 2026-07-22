package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleConfig() Config {
	return Config{
		Model: "deepseek/deepseek-v4-flash",
		Providers: []Provider{
			{
				ID:      "deepseek",
				Name:    "DeepSeek",
				BaseURL: "https://api.deepseek.com/v1",
				APIKey:  "sk-test",
				Models: map[string]ModelDef{
					"deepseek-v4-flash": {Name: "V4 Flash"},
					"deepseek-chat":     {Name: "Chat"},
				},
			},
			{
				ID:      "xai",
				Name:    "xAI",
				BaseURL: "https://api.x.ai/v1",
				APIKey:  "xai-key",
				Models: map[string]ModelDef{
					"grok-3": {Name: "Grok 3"},
				},
			},
		},
	}
}

func TestParseModelID(t *testing.T) {
	p, m, err := ParseModelID("deepseek/deepseek-v4-flash")
	if err != nil || p != "deepseek" || m != "deepseek-v4-flash" {
		t.Fatalf("got %q/%q err=%v", p, m, err)
	}
	p, m, err = ParseModelID("lmstudio/google/gemma-3n")
	if err != nil || p != "lmstudio" || m != "google/gemma-3n" {
		t.Fatalf("model id may contain slashes: got %q/%q err=%v", p, m, err)
	}
	if _, _, err := ParseModelID("nope"); err == nil {
		t.Fatal("expected error for missing slash")
	}
}

func TestDisplayNames(t *testing.T) {
	p := sampleConfig().Providers[0]
	if got := p.DisplayName(); got != "DeepSeek" {
		t.Fatalf("provider name = %q", got)
	}
	if got := p.Models["deepseek-v4-flash"].DisplayName("deepseek-v4-flash"); got != "V4 Flash" {
		t.Fatalf("model name = %q", got)
	}
	var def ModelDef
	if got := def.DisplayName("fallback"); got != "fallback" {
		t.Fatalf("default model name = %q", got)
	}
}

func TestModelChoices(t *testing.T) {
	choices := sampleConfig().ModelChoices()
	if len(choices) != 3 {
		t.Fatalf("got %d choices, want 3", len(choices))
	}
	if choices[0].ID() != "deepseek/deepseek-chat" {
		t.Fatalf("first choice id = %q", choices[0].ID())
	}
	if choices[0].Name != "DeepSeek Chat" {
		t.Fatalf("first choice name = %q", choices[0].Name)
	}
}

func TestActiveChoice(t *testing.T) {
	cfg := sampleConfig()
	ch, ok := cfg.ActiveChoice()
	if !ok || ch.ID() != "deepseek/deepseek-v4-flash" || ch.Name != "DeepSeek V4 Flash" {
		t.Fatalf("ActiveChoice = %#v ok=%v", ch, ok)
	}
}

func TestActiveModelID(t *testing.T) {
	cfg := sampleConfig()
	if got := cfg.ActiveModelID(); got != "deepseek-v4-flash" {
		t.Fatalf("got %q, want deepseek-v4-flash", got)
	}
}

func TestModelName(t *testing.T) {
	cfg := sampleConfig()
	if got := cfg.ModelName(); got != "DeepSeek V4 Flash" {
		t.Fatalf("got %q", got)
	}
}

func TestSetModel(t *testing.T) {
	cfg := sampleConfig()
	cfg.SetModel("xai/grok-3")
	if cfg.Model != "xai/grok-3" {
		t.Fatalf("SetModel = %q", cfg.Model)
	}
}

func TestValidateRequiresModels(t *testing.T) {
	cfg := sampleConfig()
	cfg.Providers[0].Models = nil
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty models")
	}
}

func TestValidateModel(t *testing.T) {
	cfg := sampleConfig()
	cfg.Model = "deepseek/nonexistent"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid model")
	}
	cfg.Model = "unknown/foo"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)

	cfg := sampleConfig()
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Model != cfg.Model {
		t.Fatalf("loaded model = %q", loaded.Model)
	}
	if len(loaded.Providers) != 2 || len(loaded.Providers[0].Models) != 2 {
		t.Fatalf("providers = %#v", loaded.Providers)
	}
	if loaded.Providers[0].Name != "DeepSeek" {
		t.Fatalf("provider name = %q", loaded.Providers[0].Name)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"models"`) {
		t.Fatalf("config should contain models key: %s", data)
	}
}
