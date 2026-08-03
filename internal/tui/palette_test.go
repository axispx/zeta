package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

func TestFormatPaletteRow(t *testing.T) {
	items := []command{
		{name: "/clear", desc: "start a new session"},
		{name: "/resume", desc: "open a previous session"},
	}
	nameW := paletteNameWidth(items)
	ink := styles.PlainOverlayInk()

	row := formatPaletteRow(nameW, items[0], true, ink)
	if !strings.Contains(row, "→") || !strings.Contains(row, "/clear") {
		t.Fatalf("selected row = %q", row)
	}
	if !strings.Contains(row, "start a new session") {
		t.Fatalf("missing desc: %q", row)
	}

	r1 := formatPaletteRow(nameW, items[0], false, ink)
	r2 := formatPaletteRow(nameW, items[1], false, ink)
	i1 := strings.Index(r1, "start")
	i2 := strings.Index(r2, "open")
	if i1 <= 0 || i1 != i2 {
		t.Fatalf("desc columns misaligned: %d vs %d\n%q\n%q", i1, i2, r1, r2)
	}
	if row == r1 {
		t.Fatalf("selected accent should differ from plain: %q", row)
	}
}

func TestFormatHintRowFitsWidth(t *testing.T) {
	ink := styles.PlainOverlayInk()
	const w = 40
	row := formatHintRow("→ ", "DeepSeek", "connected", w, ink.Current, ink.CurrentHint, ink.Gap)
	if got := lipgloss.Width(row); got > w {
		t.Fatalf("row width %d > %d: %q", got, w, row)
	}
	row = formatHintRow("  ", "Custom", "endpoint", w, ink.Row, ink.Hint, ink.Gap)
	if got := lipgloss.Width(row); got > w {
		t.Fatalf("other row width %d > %d: %q", got, w, row)
	}
}

// lipgloss v2 Width includes border; content must leave room for border+padding
// or right-side hints wrap onto the next line.
func TestConfigDialogHintStaysOnRow(t *testing.T) {
	const panelW = 40
	const padH = 4
	const borderH = 2
	contentW := panelW - padH - borderH

	ink := styles.PlainOverlayInk()
	row := formatHintRow("  ", "DeepSeek", "connected", contentW, ink.Current, ink.CurrentHint, ink.Gap)
	body := ink.Header.Render("  CONNECT") + "\n" + row + "\n" +
		formatHintRow("  ", "xAI", "", contentW, ink.Row, ink.Hint, ink.Gap)

	out := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Width(panelW).
		Render(body)

	plain := stripANSI(out)
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "DeepSeek") {
			if !strings.Contains(line, "connected") {
				t.Fatalf("connected wrapped off DeepSeek row:\n%s", plain)
			}
			return
		}
		if strings.TrimSpace(strings.Trim(line, "│ ")) == "connected" {
			t.Fatalf("connected on its own line:\n%s", plain)
		}
	}
	t.Fatalf("DeepSeek row not found:\n%s", plain)
}

func TestFormatAccentRowTaggedShowsCustom(t *testing.T) {
	ink := styles.PlainOverlayInk()
	row := formatAccentRowTagged("Local", " (Custom)", "2", 40, false, false, ink)
	plain := stripANSI(row)
	if !strings.Contains(plain, "Local") || !strings.Contains(plain, "(Custom)") {
		t.Fatalf("missing custom tag: %q", plain)
	}
	if !strings.Contains(plain, "2") {
		t.Fatalf("missing count: %q", plain)
	}
}
