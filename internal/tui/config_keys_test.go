package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/styles"
)

var (
	escPress   = tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"}
	ctrlUPress = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
)

func keysTestDialog(view configView) configDialog {
	return configDialog{
		active:  true,
		view:    view,
		focusID: "p",
		draft: config.Config{
			Providers: map[string]config.Provider{
				"p": {
					Name: "P", BaseURL: "https://p/v1", APIKey: "k",
					Models: map[string]config.ModelDef{"m1": {Name: "M1", ContextWindow: 1000}},
				},
			},
		},
	}
}

// esc is one rung, so a filtered model list steps back rather than emptying
// the search box first.
func TestEscFromModelsGoesBackWithFilterIntact(t *testing.T) {
	d := keysTestDialog(configModels)
	d.presetQuery = "pp"
	d.modelQuery = "m"

	d.handleKey(escPress)

	if d.view != configPresets {
		t.Fatalf("view = %v", d.view)
	}
	if d.presetQuery != "pp" {
		t.Fatalf("presetQuery = %q", d.presetQuery)
	}
}

// esc at the root closes on the first press, filtered or not.
func TestEscFromPresetsClosesWithFilterSet(t *testing.T) {
	d := keysTestDialog(configPresets)
	d.presetQuery = "pp"

	d.handleKey(escPress)

	if d.active {
		t.Fatal("dialog still open")
	}
}

// ctrl+u is what clears a search box now that esc does not.
func TestCtrlUClearsSearchWithoutLeavingView(t *testing.T) {
	d := keysTestDialog(configPresets)
	d.presetQuery = "pp"
	d.handleKey(ctrlUPress)
	if d.presetQuery != "" || d.view != configPresets || !d.active {
		t.Fatalf("presets: query=%q view=%v active=%v", d.presetQuery, d.view, d.active)
	}

	d = keysTestDialog(configModels)
	d.modelQuery = "m"
	d.handleKey(ctrlUPress)
	if d.modelQuery != "" || d.view != configModels {
		t.Fatalf("models: query=%q view=%v", d.modelQuery, d.view)
	}
}

// The header says what esc does, since it does different things per view.
func TestPanelTitlesNameTheEscapeAction(t *testing.T) {
	d := keysTestDialog(configPresets)
	body, _ := d.presetsBody(60, styles.Chrome{}, styles.PlainOverlayInk())
	if got := stripANSI(body); !strings.Contains(got, "esc close") {
		t.Fatalf("presets title:\n%s", got)
	}

	d = keysTestDialog(configModels)
	body, _ = d.modelsBody(60, styles.Chrome{}, styles.PlainOverlayInk())
	if got := stripANSI(body); !strings.Contains(got, "esc back") {
		t.Fatalf("models title:\n%s", got)
	}
}

// With esc no longer clearing it, the way to empty a search box is advertised
// while there is something to clear.
func TestClearHintShowsOnlyWhileFiltering(t *testing.T) {
	d := keysTestDialog(configModels)
	_, footer := d.modelsBody(60, styles.Chrome{}, styles.PlainOverlayInk())
	if strings.Contains(stripANSI(footer.Hint), "ctrl+u") {
		t.Fatalf("unfiltered footer offers clear: %q", stripANSI(footer.Hint))
	}

	d.modelQuery = "m"
	_, footer = d.modelsBody(60, styles.Chrome{}, styles.PlainOverlayInk())
	if !strings.Contains(stripANSI(footer.Hint), "ctrl+u") {
		t.Fatalf("filtered footer lacks clear: %q", stripANSI(footer.Hint))
	}
}
