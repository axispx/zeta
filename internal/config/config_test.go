package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/models"
)

func sampleConfig() Config {
	return Config{
		Active: "deepseek/deepseek-v4-flash",
		Providers: map[string]Provider{
			"deepseek": {
				Name:    "DeepSeek",
				BaseURL: "https://api.deepseek.com/v1",
				APIKey:  "sk-test",
				Models: map[string]ModelDef{
					"deepseek-v4-flash": {Name: "V4 Flash", ContextWindow: 128000},
					"deepseek-chat":     {Name: "Chat", ContextWindow: 64000},
				},
			},
			"xai": {
				Name:    "xAI",
				BaseURL: "https://api.x.ai/v1",
				APIKey:  "xai-key",
				Models: map[string]ModelDef{
					"grok-3": {Name: "Grok 3", ContextWindow: 131072},
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
	p := sampleConfig().Providers["deepseek"]
	if got := p.DisplayName("deepseek"); got != "DeepSeek" {
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
	want := []string{
		"deepseek/deepseek-chat",
		"deepseek/deepseek-v4-flash",
		"xai/grok-3",
	}
	for i, id := range want {
		if choices[i].ID() != id {
			t.Fatalf("choices[%d] = %q, want %q", i, choices[i].ID(), id)
		}
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

func TestSetActive(t *testing.T) {
	cfg := sampleConfig()
	cfg.SetActive("xai/grok-3")
	if cfg.Active != "xai/grok-3" {
		t.Fatalf("SetActive = %q", cfg.Active)
	}
}

func TestValidateAllowsEmptyModels(t *testing.T) {
	cfg := sampleConfig()
	p := cfg.Providers["deepseek"]
	p.Models = map[string]ModelDef{}
	cfg.Providers["deepseek"] = p
	cfg.Active = "xai/grok-3"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRequiresContextWindow(t *testing.T) {
	cfg := sampleConfig()
	p := cfg.Providers["deepseek"]
	m := p.Models["deepseek-v4-flash"]
	m.ContextWindow = 0
	p.Models["deepseek-v4-flash"] = m
	cfg.Providers["deepseek"] = p
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing context_window")
	}
}

func TestContextWindow(t *testing.T) {
	cfg := sampleConfig()
	if got := cfg.ContextWindow(); got != 128000 {
		t.Fatalf("ContextWindow = %d, want 128000", got)
	}
}

func TestValidateModel(t *testing.T) {
	cfg := sampleConfig()
	cfg.Active = "deepseek/nonexistent"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid model")
	}
	cfg.Active = "unknown/foo"
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
	if loaded.Active != cfg.Active {
		t.Fatalf("loaded model = %q", loaded.Active)
	}
	if len(loaded.Providers) != 2 || len(loaded.Providers["deepseek"].Models) != 2 {
		t.Fatalf("providers = %#v", loaded.Providers)
	}
	if loaded.Providers["deepseek"].Name != "DeepSeek" {
		t.Fatalf("provider name = %q", loaded.Providers["deepseek"].Name)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"models"`) {
		t.Fatalf("config should contain models key: %s", data)
	}
	if strings.Contains(s, `"id":`) {
		t.Fatalf("provider id should be the map key, not a field: %s", data)
	}
}

func TestLoadRejectsProviderArray(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)
	raw := `{
  "active": "deepseek/deepseek-v4-flash",
  "providers": [
    {
      "id": "deepseek",
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "sk-test",
      "models": {
        "deepseek-v4-flash": {"context_window": 128000}
      }
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for providers array")
	}
	if !strings.Contains(err.Error(), "unsupported config") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("err = %v", err)
	}
}

func TestClone(t *testing.T) {
	cfg := sampleConfig()
	cl := cfg.Clone()
	p := cl.Providers["deepseek"]
	p.APIKey = "changed"
	p.Models["deepseek-v4-flash"] = ModelDef{Name: "X", ContextWindow: 1}
	cl.Providers["deepseek"] = p
	if cfg.Providers["deepseek"].APIKey != "sk-test" {
		t.Fatal("Clone mutated original API key")
	}
	if cfg.Providers["deepseek"].Models["deepseek-v4-flash"].Name != "V4 Flash" {
		t.Fatal("Clone mutated original model")
	}
}

func TestPutProvider(t *testing.T) {
	cfg := sampleConfig()
	if err := cfg.PutProvider("deepseek", Provider{
		Name: "DS", BaseURL: "https://x", APIKey: "new", Models: map[string]ModelDef{},
	}); err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("deepseek")
	if !ok || p.Name != "DS" || p.APIKey != "new" || len(p.Models) != 0 {
		t.Fatalf("PutProvider should replace models: %#v", p)
	}
	if err := cfg.PutProvider("openai", Provider{
		Name: "OpenAI", BaseURL: "https://api.openai.com/v1", APIKey: "k",
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Provider("openai"); !ok || len(cfg.Providers) != 3 {
		t.Fatal("expected new provider")
	}
}

func TestUpdateProvider(t *testing.T) {
	cfg := sampleConfig()
	if err := cfg.UpdateProvider("deepseek", "DS", "https://x", "new"); err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("deepseek")
	if !ok || p.Name != "DS" || p.BaseURL != "https://x" || p.APIKey != "new" || len(p.Models) != 2 {
		t.Fatalf("UpdateProvider should preserve models: %#v", p)
	}
	if err := cfg.UpdateProvider("deepseek", "", "", "only-key"); err != nil {
		t.Fatal(err)
	}
	p, ok = cfg.Provider("deepseek")
	if !ok || p.Name != "DS" || p.BaseURL != "https://x" || p.APIKey != "only-key" {
		t.Fatalf("empty args should keep existing: %#v", p)
	}
}

func TestDeleteProvider(t *testing.T) {
	cfg := sampleConfig()
	if err := cfg.DeleteProvider("deepseek"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Provider("deepseek"); ok {
		t.Fatal("deepseek should be gone")
	}
	if cfg.Active != "xai/grok-3" {
		t.Fatalf("Active = %q, want xai/grok-3", cfg.Active)
	}
	if err := cfg.DeleteProvider("xai"); err != nil {
		t.Fatal(err)
	}
	if cfg.Active != "" {
		t.Fatalf("Active = %q, want empty", cfg.Active)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertModel(t *testing.T) {
	cfg := Config{}
	_ = cfg.PutProvider("p", Provider{
		BaseURL: "https://x", APIKey: "k", Models: map[string]ModelDef{},
	})
	if err := cfg.UpsertModel("p", "m1", ModelDef{Name: "M1", ContextWindow: 8_000}); err != nil {
		t.Fatal(err)
	}
	if cfg.Active != "p/m1" {
		t.Fatalf("bootstrap Model = %q", cfg.Active)
	}
	if err := cfg.UpsertModel("p", "m1", ModelDef{Name: "M1b", ContextWindow: 16_000}); err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["p"].Models["m1"].Name != "M1b" {
		t.Fatal("expected model update")
	}
}

func TestSetModelEnabled(t *testing.T) {
	cfg := Config{}
	_ = cfg.PutProvider("p", Provider{
		BaseURL: "https://x", APIKey: "k", Models: map[string]ModelDef{},
	})
	_ = cfg.UpsertModel("p", "m1", ModelDef{ContextWindow: 8_000})
	_ = cfg.UpsertModel("p", "m2", ModelDef{ContextWindow: 8_000})
	cfg.Active = "p/m1"
	if err := cfg.SetModelEnabled("p", "m1", false); err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["p"].Models["m1"].Enabled() {
		t.Fatal("m1 should be disabled")
	}
	if cfg.Active != "p/m2" {
		t.Fatalf("Active = %q, want p/m2", cfg.Active)
	}
	for _, ch := range cfg.ModelChoices() {
		if ch.ModelID == "m1" {
			t.Fatal("disabled model should not appear in ModelChoices")
		}
	}
	if err := cfg.SetModelEnabled("p", "m1", true); err != nil {
		t.Fatal(err)
	}
	if !cfg.Providers["p"].Models["m1"].Enabled() {
		t.Fatal("m1 should be enabled")
	}
}

func TestDeleteModel(t *testing.T) {
	cfg := sampleConfig()
	if err := cfg.DeleteModel("deepseek", "deepseek-v4-flash"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Providers["deepseek"].Models["deepseek-v4-flash"]; ok {
		t.Fatal("active model should be gone")
	}
	if cfg.Active != "deepseek/deepseek-chat" {
		t.Fatalf("Active = %q, want deepseek/deepseek-chat", cfg.Active)
	}
	if err := cfg.DeleteModel("deepseek", "deepseek-chat"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Provider("deepseek"); !ok {
		t.Fatal("provider should remain with no models")
	}
	if len(cfg.Providers["deepseek"].Models) != 0 {
		t.Fatal("expected empty models")
	}
	if cfg.Active != "xai/grok-3" {
		t.Fatalf("Active = %q, want xai/grok-3", cfg.Active)
	}
}

func TestConnectPreset(t *testing.T) {
	cfg := Config{}
	pre := Preset{
		ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1",
		DefaultModel: "deepseek-v4-flash",
		Models: map[string]ModelDef{
			"deepseek-v4-flash": {Name: "V4 Flash", ContextWindow: 1_000_000},
			"deepseek-v4-pro":   {Name: "V4 Pro", ContextWindow: 1_000_000},
		},
	}
	if err := cfg.ConnectPreset(pre, "sk-test"); err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("deepseek")
	if !ok || p.APIKey != "sk-test" || p.BaseURL == "" {
		t.Fatalf("provider = %#v ok=%v", p, ok)
	}
	if len(p.Models) != len(pre.Models) {
		t.Fatalf("expected full catalog materialized, got %#v", p.Models)
	}
	for _, m := range p.Models {
		if m.Enabled() {
			t.Fatalf("new connect should disable all models, got %#v", p.Models)
		}
	}
	if cfg.Active != "" {
		t.Fatalf("Active = %q, want empty until enable", cfg.Active)
	}
	if err := cfg.SetAllModelsEnabled("deepseek", true); err != nil {
		t.Fatal(err)
	}
	if cfg.Active != "deepseek/deepseek-v4-flash" && cfg.Active != "deepseek/deepseek-v4-pro" {
		t.Fatalf("Active = %q", cfg.Active)
	}
	for _, m := range cfg.Providers["deepseek"].Models {
		if !m.Enabled() {
			t.Fatal("expected all enabled")
		}
	}
}

func TestSyncCatalogModels(t *testing.T) {
	cfg := Config{}
	pre := Preset{
		ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1",
		Models: map[string]ModelDef{
			"deepseek-v4-flash": {Name: "V4 Flash", ContextWindow: 1_000_000},
			"deepseek-v4-pro":   {Name: "V4 Pro", ContextWindow: 1_000_000},
		},
	}
	_ = cfg.ConnectPreset(pre, "sk")
	_ = cfg.SetModelEnabled("deepseek", "deepseek-v4-flash", true)

	catalog := map[string]ModelDef{
		"deepseek-v4-flash": {Name: "Flash", ContextWindow: 2_000_000},
		"new-model":         {Name: "New", ContextWindow: 8_000},
	}
	if err := cfg.SyncCatalogModels("deepseek", catalog); err != nil {
		t.Fatal(err)
	}
	p := cfg.Providers["deepseek"]
	if !p.Models["deepseek-v4-flash"].Enabled() {
		t.Fatal("enabled flag should be preserved")
	}
	if p.Models["deepseek-v4-flash"].ContextWindow != 2_000_000 {
		t.Fatal("def should refresh from catalog")
	}
	if p.Models["new-model"].Enabled() {
		t.Fatal("new catalog models should start disabled")
	}
	if _, ok := p.Models["deepseek-v4-pro"]; ok {
		t.Fatal("models absent from catalog should be dropped")
	}
}

func TestConnectCustom(t *testing.T) {
	cfg := Config{}
	if err := cfg.ConnectCustom("local", "", "http://localhost:1234/v1", "x"); err != nil {
		t.Fatal(err)
	}
	p := cfg.Providers["local"]
	if !p.Custom || len(p.Models) != 0 {
		t.Fatalf("expected custom empty provider, got %#v", p)
	}
	if cfg.Active != "" {
		t.Fatalf("Active = %q, want empty", cfg.Active)
	}
	if err := cfg.UpsertModel("local", "m1", ModelDef{ContextWindow: 128_000}); err != nil {
		t.Fatal(err)
	}
	if cfg.Active != "local/m1" {
		t.Fatalf("Active = %q", cfg.Active)
	}

	err := cfg.ConnectCustom("local", "", "http://localhost:1/v1", "k")
	if err == nil || !strings.Contains(err.Error(), `"local" is an existing provider id`) {
		t.Fatalf("expected existing id error, got %v", err)
	}
}

func TestConnectPresetNotCustom(t *testing.T) {
	cfg := Config{}
	pre := Preset{
		ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1",
		Models: map[string]ModelDef{"m": {ContextWindow: 1000}},
	}
	if err := cfg.ConnectPreset(pre, "sk"); err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["deepseek"].Custom {
		t.Fatal("catalog connect should not set Custom")
	}
}

func TestPresetsFromModels(t *testing.T) {
	in := []models.Preset{{
		ID: "x", Name: "X", BaseURL: "https://x/v1", DefaultModel: "m",
		Models: map[string]models.ModelInfo{"m": {Name: "M", ContextWindow: 8_000}},
	}}
	out := PresetsFromModels(in)
	if len(out) != 1 || out[0].ID != "x" || out[0].Models["m"].ContextWindow != 8_000 {
		t.Fatalf("%#v", out)
	}
}
