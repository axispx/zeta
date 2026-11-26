package tui

import (
	"testing"

	"github.com/axispx/zeta/internal/config"
)

func refreshCfg() config.Config {
	return config.Config{
		Active: "x/old",
		Providers: map[string]config.Provider{
			"x": {
				BaseURL: "http://127.0.0.1:1",
				APIKey:  "k",
				Models: map[string]config.ModelDef{
					"old":  {ContextWindow: 100},
					"keep": {ContextWindow: 100, Disabled: true},
				},
			},
			"mine": {
				BaseURL: "http://127.0.0.1:2",
				APIKey:  "k",
				Custom:  true,
				Models:  map[string]config.ModelDef{"m": {ContextWindow: 5}},
			},
		},
	}
}

func TestModelsRefreshMergesCatalog(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	m := testModel()
	m.session.Cfg = refreshCfg()

	m.handleModelsRefreshed(modelsRefreshedMsg{presets: map[string]config.Preset{
		"x": {ID: "x", Models: map[string]config.ModelDef{
			"keep": {ContextWindow: 200},
			"new":  {ContextWindow: 300},
		}},
	}})

	got, _ := m.session.Cfg.Provider("x")
	if got.Models["keep"].ContextWindow != 200 || !got.Models["keep"].Disabled {
		t.Errorf("keep = %+v, want new window, still disabled", got.Models["keep"])
	}
	if nm, ok := got.Models["new"]; !ok || !nm.Disabled {
		t.Errorf("new = %+v, %v; want present and disabled", nm, ok)
	}
	// The active model left the catalog but stays so the session keeps it.
	if _, ok := got.Models["old"]; !ok {
		t.Error("active model dropped")
	}
	if m.session.Cfg.Active != "x/old" {
		t.Errorf("Active = %q", m.session.Cfg.Active)
	}
	if mine, _ := m.session.Cfg.Provider("mine"); mine.Models["m"].ContextWindow != 5 {
		t.Errorf("custom provider touched: %+v", mine)
	}
	if len(m.transcript.messages) != 0 {
		t.Errorf("refresh should be silent: %+v", m.transcript.messages)
	}
}

func TestModelsRefreshWithoutCatalogChangesNothing(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	m := testModel()
	m.session.Cfg = refreshCfg()
	before := m.session.Cfg.Clone()

	// No entry for the provider, and an empty one: both leave the list alone.
	m.handleModelsRefreshed(modelsRefreshedMsg{presets: map[string]config.Preset{
		"x": {ID: "x"},
	}})

	got, _ := m.session.Cfg.Provider("x")
	want, _ := before.Provider("x")
	if len(got.Models) != len(want.Models) {
		t.Fatalf("models = %+v, want %+v", got.Models, want.Models)
	}
}

func TestRefreshModelsCmdSkipsWhenNothingToRefresh(t *testing.T) {
	cfg := config.Config{Providers: map[string]config.Provider{
		"mine": {BaseURL: "http://127.0.0.1:2", APIKey: "k", Custom: true},
	}}
	if refreshModelsCmd(cfg) != nil {
		t.Fatal("custom-only config should not refresh")
	}
	if refreshModelsCmd(config.Config{}) != nil {
		t.Fatal("empty config should not refresh")
	}
}
