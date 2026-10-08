package tui

import (
	"testing"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
)

func codexFastModel(t *testing.T, tier string) *Model {
	t.Helper()
	t.Setenv("ZETA_HOME", t.TempDir())
	m := testModel()
	m.session.Cfg = config.Config{
		Active: "openai/gpt-6",
		Providers: map[string]config.Provider{
			"openai": {
				Name:    "OpenAI",
				BaseURL: codex.BaseURL,
				OAuth:   &config.OAuthCredential{AccessToken: "tok", AccountID: "acct"},
				Models:  map[string]config.ModelDef{"gpt-6": {ContextWindow: 272000, FastTier: tier}},
			},
		},
	}
	return m
}

func TestToggleFastWithKnownTier(t *testing.T) {
	m := codexFastModel(t, "priority")
	if cmd := m.toggleFast(); cmd != nil {
		t.Fatal("a known tier needs no catalog read")
	}
	if !m.session.Cfg.ActiveFast() {
		t.Fatal("fast should be on")
	}
	m.toggleFast()
	if m.session.Cfg.ActiveFast() {
		t.Fatal("fast should be off")
	}
}

func TestToggleFastReadsCatalogWhenTierUnknown(t *testing.T) {
	m := codexFastModel(t, "")
	if cmd := m.toggleFast(); cmd == nil {
		t.Fatal("an unknown tier should read the catalog")
	}
	if m.session.Cfg.ActiveFast() {
		t.Fatal("fast must wait for the catalog")
	}

	m.handleFastTiers(fastTiersMsg{
		choice: config.ModelChoice{ProviderID: "openai", ModelID: "gpt-6", Name: "OpenAI gpt-6"},
		models: []codex.Model{{Slug: "gpt-6", FastTier: "priority"}},
	})
	if !m.session.Cfg.ActiveFast() {
		t.Fatal("fast should be on after the catalog reports a tier")
	}
}

func TestHandleFastTiersWithoutTier(t *testing.T) {
	m := codexFastModel(t, "")
	m.handleFastTiers(fastTiersMsg{
		choice: config.ModelChoice{ProviderID: "openai", ModelID: "gpt-6", Name: "OpenAI gpt-6"},
		models: []codex.Model{{Slug: "gpt-6"}},
	})
	if m.session.Cfg.ActiveFast() {
		t.Fatal("a model without a tier must stay off")
	}
}

func TestToggleFastNonCodexWithoutTier(t *testing.T) {
	m := testModel()
	m.session.Cfg = config.Config{
		Active: "x/y",
		Providers: map[string]config.Provider{
			"x": {BaseURL: "https://api.example.com/v1", Models: map[string]config.ModelDef{"y": {ContextWindow: 1}}},
		},
	}
	if cmd := m.toggleFast(); cmd != nil {
		t.Fatal("non-Codex providers have no catalog to read")
	}
	if m.session.Cfg.ActiveFast() {
		t.Fatal("fast must stay off")
	}
}
