package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/styles"
)

func TestProviderCaps(t *testing.T) {
	d := configDialog{
		draft: config.Config{
			Providers: map[string]config.Provider{
				"openai": {BaseURL: "https://x", APIKey: "k", Custom: false},
				"local":  {BaseURL: "http://localhost", APIKey: "k", Custom: true},
			},
		},
	}
	if d.caps("openai").custom || d.caps("openai").canRename() || !d.caps("openai").canToggleAll() {
		t.Fatalf("catalog caps = %#v", d.caps("openai"))
	}
	if !d.caps("local").custom || !d.caps("local").canEditModels() || d.caps("local").canToggleAll() {
		t.Fatalf("custom caps = %#v", d.caps("local"))
	}
	// Caps stay durable when presets are empty (catalog load failed).
	d.presets = nil
	if d.caps("openai").canRename() || !d.caps("openai").canToggleAll() {
		t.Fatalf("catalog caps without presets = %#v", d.caps("openai"))
	}
}

func TestMutateAtomicOnValidateFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)

	d := configDialog{
		draft: config.Config{
			Active: "p/m1",
			Providers: map[string]config.Provider{
				"p": {
					BaseURL: "http://localhost",
					APIKey:  "k",
					Models:  map[string]config.ModelDef{"m1": {ContextWindow: 1000}},
				},
			},
		},
	}
	before := d.draft.Clone()
	err := d.mutate(func(c *config.Config) error {
		c.Active = "not-a-valid-id"
		return nil
	})
	if err == nil {
		t.Fatal("expected validate error")
	}
	if d.draft.Active != before.Active {
		t.Fatalf("draft mutated on validate failure: %q", d.draft.Active)
	}
}

func TestMutateAtomicOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	badHome := filepath.Join(dir, "notadir")
	if err := os.WriteFile(badHome, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZETA_HOME", badHome)

	d := configDialog{
		draft: config.Config{
			Providers: map[string]config.Provider{
				"p": {
					BaseURL: "http://localhost",
					APIKey:  "k",
					Models:  map[string]config.ModelDef{"m1": {ContextWindow: 1000}},
				},
			},
		},
	}
	before := d.draft.Active
	err := d.mutate(func(c *config.Config) error {
		c.Active = "p/m1"
		return nil
	})
	if err == nil {
		t.Fatal("expected save error")
	}
	if d.draft.Active != before {
		t.Fatalf("draft swapped despite save failure: %q", d.draft.Active)
	}
	if d.takeSaved() != nil {
		t.Fatal("nothing should be published on save failure")
	}
}

func TestMutateFnErrorLeavesDraft(t *testing.T) {
	d := configDialog{
		draft: config.Config{Active: "keep"},
	}
	err := d.mutate(func(*config.Config) error {
		return errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v", err)
	}
	if d.draft.Active != "keep" {
		t.Fatalf("Active = %q", d.draft.Active)
	}
}

func TestConnectRowsKinds(t *testing.T) {
	d := configDialog{
		presets: []config.Preset{
			{ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1", Models: map[string]config.ModelDef{"m": {ContextWindow: 1000}}},
		},
		draft: config.Config{
			Providers: map[string]config.Provider{
				"local": {
					Name: "Local", BaseURL: "http://localhost", APIKey: "k", Custom: true,
					Models: map[string]config.ModelDef{"m1": {ContextWindow: 1000}},
				},
			},
		},
	}
	rows := d.connectRows()
	var kinds []connectKind
	for _, r := range rows {
		kinds = append(kinds, r.kind)
	}
	if len(kinds) < 3 {
		t.Fatalf("kinds = %v", kinds)
	}
	if kinds[0] != connectConfigured || rows[0].tag != " (Custom)" {
		t.Fatalf("first = %#v", rows[0])
	}
	foundCatalog, foundCustom := false, false
	for _, r := range rows {
		if r.kind == connectCatalog {
			foundCatalog = true
		}
		if r.kind == connectCustom {
			foundCustom = true
		}
	}
	if !foundCatalog || !foundCustom {
		t.Fatalf("missing rows: %#v", rows)
	}
}

func TestPresetsBodyShowsCustomTag(t *testing.T) {
	d := configDialog{
		active: true,
		view:   configPresets,
		draft: config.Config{
			Providers: map[string]config.Provider{
				"local": {
					Name: "Local", BaseURL: "http://localhost", APIKey: "k", Custom: true,
					Models: map[string]config.ModelDef{"m1": {ContextWindow: 1000}},
				},
			},
		},
	}
	body, _ := d.presetsBody(48, styles.Chrome{}, styles.PlainOverlayInk())
	plain := stripANSI(body)
	if !strings.Contains(plain, "Local") || !strings.Contains(plain, "(Custom)") {
		t.Fatalf("expected Local (Custom) in body:\n%s", plain)
	}
}

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"}
)

// stepModel drives one msg through Update the way Bubble Tea does: on a copy,
// keeping only what Update returns. Writes the dialog makes through a pointer
// captured in an earlier turn are dropped here, as they are at runtime.
func stepModel(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	return got
}

// Enabling a model must reach the running Model, not just ~/.zeta/config.json.
func TestConfigDialogSaveReachesLiveModel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)

	m := testModel()
	m.cfg = config.Config{Providers: map[string]config.Provider{
		"p": {
			Name: "P", BaseURL: "https://p/v1", APIKey: "k",
			Models: map[string]config.ModelDef{"m1": {Name: "M1", ContextWindow: 1000, Disabled: true}},
		},
	}}
	m.applyClient()
	if m.client != nil {
		t.Fatal("no model enabled; client should start nil")
	}

	m.textarea.SetValue("/config")
	m = stepModel(t, m, keyEnter)
	if !m.config.active {
		t.Fatal("/config did not open the dialog")
	}
	// Stand in for the models.dev fetch so the list is not stuck loading.
	m = stepModel(t, m, modelsDevLoadedMsg{gen: m.config.loadGen, presets: []config.Preset{{
		ID: "p", Name: "P", BaseURL: "https://p/v1",
		Models: map[string]config.ModelDef{"m1": {Name: "M1", ContextWindow: 1000}},
	}}})
	if m.config.loading {
		t.Fatal("presets still loading")
	}

	m = stepModel(t, m, keyEnter) // configured row 0 → its models
	if m.config.view != configModels {
		t.Fatalf("view = %v", m.config.view)
	}
	m = stepModel(t, m, keyEnter) // enable m1
	m = stepModel(t, m, keyEsc)   // models → presets
	m = stepModel(t, m, keyEsc)   // presets → closed

	if m.config.active {
		t.Fatal("dialog still open")
	}
	if m.cfg.Active != "p/m1" {
		t.Fatalf("Active = %q", m.cfg.Active)
	}
	if m.client == nil {
		t.Fatal("client nil; the save never reached the live model")
	}
}

func TestOpenModelsSyncsCatalogOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)

	d := configDialog{
		presets: []config.Preset{{
			ID: "p", Name: "P", BaseURL: "https://p/v1",
			Models: map[string]config.ModelDef{
				"m1": {Name: "M1", ContextWindow: 1000},
				"m2": {Name: "M2", ContextWindow: 2000},
			},
		}},
		draft: config.Config{
			Providers: map[string]config.Provider{
				"p": {
					BaseURL: "https://p/v1", APIKey: "k",
					Models: map[string]config.ModelDef{
						"m1": {Name: "Old", ContextWindow: 500, Disabled: false},
					},
				},
			},
		},
	}
	d.openModels("p")
	p := d.draft.Providers["p"]
	if len(p.Models) != 2 {
		t.Fatalf("expected sync to materialize catalog, got %#v", p.Models)
	}
	if p.Models["m1"].Disabled {
		t.Fatal("enabled flag should be preserved")
	}
	if p.Models["m1"].ContextWindow != 1000 {
		t.Fatalf("def should refresh from catalog: %#v", p.Models["m1"])
	}
	if !p.Models["m2"].Disabled {
		t.Fatal("new catalog model should start disabled")
	}
}
