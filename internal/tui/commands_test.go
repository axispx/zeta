package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
)

func TestIsSlashToken(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"/clear", true},
		{"/resume", true},
		{"  /clear  ", true},
		{"/clear foo", false},
		{"hello", false},
		{"/", true},
		{"/cle", true},
	}
	for _, tt := range tests {
		if got := isSlashToken(tt.in); got != tt.want {
			t.Errorf("isSlashToken(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestLookupCommand(t *testing.T) {
	if _, ok := lookupCommand("/clear"); !ok {
		t.Fatal("expected /clear")
	}
	if _, ok := lookupCommand("/model"); !ok {
		t.Fatal("expected /model")
	}
	if _, ok := lookupCommand("/cle"); ok {
		t.Fatal("partial should not match")
	}
	if _, ok := lookupCommand("/foo"); ok {
		t.Fatal("unknown should not match")
	}
}

func TestMatchCommands(t *testing.T) {
	all := matchCommands("/")
	if len(all) != 3 {
		t.Fatalf("match / = %d items", len(all))
	}
	clear := matchCommands("/cle")
	if len(clear) != 1 || clear[0].name != "/clear" {
		t.Fatalf("match /cle = %#v", clear)
	}
	none := matchCommands("/foo")
	if len(none) != 0 {
		t.Fatalf("match /foo = %#v", none)
	}
}

func TestListSelMove(t *testing.T) {
	var l listSel
	if !l.move(3, "down") || l.selected != 1 {
		t.Fatalf("down = %d", l.selected)
	}
	if !l.move(3, "down") || l.selected != 2 {
		t.Fatalf("down2 = %d", l.selected)
	}
	if !l.move(3, "down") || l.selected != 2 {
		t.Fatalf("down clamp = %d", l.selected)
	}
	if !l.move(3, "ctrl+n") || l.selected != 2 {
		t.Fatalf("ctrl+n = %d", l.selected)
	}
	if !l.move(3, "ctrl+p") || l.selected != 1 {
		t.Fatalf("ctrl+p = %d", l.selected)
	}
	if l.move(3, "enter") {
		t.Fatal("enter should not be nav")
	}
}

func TestModelLabelAccent(t *testing.T) {
	if got := modelLabelAccent(true, true); got != modelAccentCurrent {
		t.Fatalf("selected+current keeps active color: %v", got)
	}
	if got := modelLabelAccent(true, false); got != modelAccentSelected {
		t.Fatalf("selected: %v", got)
	}
	if got := modelLabelAccent(false, true); got != modelAccentCurrent {
		t.Fatalf("current: %v", got)
	}
	if got := modelLabelAccent(false, false); got != modelAccentNone {
		t.Fatalf("none: %v", got)
	}
}

func TestFormatModelRowSelected(t *testing.T) {
	row := formatModelRow("GPT-4", "", 40, true, false)
	if !strings.Contains(row, "→") {
		t.Fatalf("expected arrow for selected row: %q", row)
	}
}

func TestFormatPickerRow(t *testing.T) {
	row := formatPickerRow("refactor auth", "2h ago", 40, false)
	if !strings.Contains(row, "refactor auth") {
		t.Fatalf("label not left: %q", row)
	}
	if !strings.Contains(row, "2h ago") {
		t.Fatalf("time not right: %q", row)
	}
	long := formatPickerRow(strings.Repeat("x", 50), "just now", 30, true)
	if lipgloss.Width(long) > 30 {
		t.Fatalf("row too wide: %d", lipgloss.Width(long))
	}
}

func TestSyncOverlaySelectsPartial(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/cle")
	m := Model{textarea: ta}
	m.syncOverlay()
	if !m.overlay.showing() || m.overlay.mode != overlayCommands {
		t.Fatal("command overlay inactive")
	}
	if m.overlay.cmds[m.overlay.selected].name != "/clear" {
		t.Fatalf("selected = %#v", m.overlay.cmds)
	}
}

func TestVisibleModels(t *testing.T) {
	o := filterOverlay{
		models: []config.ModelChoice{
			{ProviderID: "openai", ModelID: "gpt-4", Name: "GPT-4"},
			{ProviderID: "deepseek", ModelID: "v3", Name: "DeepSeek V3"},
		},
	}
	visible := o.visibleModels("deep")
	if len(visible) != 1 || visible[0].ModelID != "v3" {
		t.Fatalf("visible = %#v", visible)
	}
}

func TestHandleModelOverlayKey(t *testing.T) {
	m := Model{
		overlay: filterOverlay{
			mode: overlayModels,
			models: []config.ModelChoice{
				{ProviderID: "a", ModelID: "1", Name: "Alpha"},
				{ProviderID: "b", ModelID: "2", Name: "Beta"},
			},
		},
	}
	m.overlay.selected = 1

	if _, ok := m.handleOverlayKey(tea.KeyPressMsg{Code: tea.KeyUp}); !ok {
		t.Fatal("up should be consumed")
	}
	if m.overlay.selected != 0 {
		t.Fatalf("up selected = %d", m.overlay.selected)
	}

	if _, ok := m.handleOverlayKey(tea.KeyPressMsg{Code: tea.KeyDown}); !ok {
		t.Fatal("down should be consumed")
	}
	if m.overlay.selected != 1 {
		t.Fatalf("down selected = %d", m.overlay.selected)
	}

	if _, ok := m.handleOverlayKey(tea.KeyPressMsg{Code: 'x', Text: "x"}); ok {
		t.Fatal("typing should not be consumed")
	}
}

func TestWindowAround(t *testing.T) {
	start, end := windowAround(0, 3, 5)
	if start != 0 || end != 3 {
		t.Fatalf("short list: %d,%d", start, end)
	}
	start, end = windowAround(4, 10, 5)
	if end-start != 5 || start > 4 || end <= 4 {
		t.Fatalf("window: %d,%d", start, end)
	}
}

func TestRenderModelOverlayMaxRows(t *testing.T) {
	entries := make([]config.ModelChoice, 8)
	for i := range entries {
		entries[i] = config.ModelChoice{
			ProviderID: "p",
			ModelID:    string(rune('a' + i)),
			Name:       string(rune('A' + i)),
		}
	}
	m := Model{
		cfg: config.Config{Model: "p/a"},
		overlay: filterOverlay{
			mode:   overlayModels,
			models: entries,
		},
	}
	out := m.renderModelOverlay(80)
	lines := strings.Split(out, "\n")
	if len(lines) > modelOverlayMaxRows {
		t.Fatalf("got %d lines, want <= %d", len(lines), modelOverlayMaxRows)
	}
}
