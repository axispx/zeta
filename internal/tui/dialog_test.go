package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

func TestDialogFitWidth(t *testing.T) {
	d := NewDialog(styles.Chrome{})
	panelW, contentW := d.FitWidth(80)
	if panelW != dialogDefaultMaxW {
		t.Fatalf("panelW = %d, want %d", panelW, dialogDefaultMaxW)
	}
	if contentW != panelW-dialogPadH-dialogBorderH {
		t.Fatalf("contentW = %d", contentW)
	}
	panelW, _ = d.FitWidth(30)
	if panelW > 30 {
		t.Fatalf("panel should clamp to term: %d", panelW)
	}
}

func TestDialogPlaceFillsScrim(t *testing.T) {
	d := Dialog{
		MaxWidth: 40,
		MinWidth: 20,
		PanelBG:  lipgloss.Color("0"),
		ScrimBG:  lipgloss.Color("0"),
		BorderFG: styles.Dim,
	}
	panel := d.Panel("hello", 24)
	out := d.Place(40, 10, panel)
	if lipgloss.Width(out) != 40 {
		t.Fatalf("width = %d, want 40", lipgloss.Width(out))
	}
	if lipgloss.Height(out) != 10 {
		t.Fatalf("height = %d, want 10", lipgloss.Height(out))
	}
	if !strings.Contains(stripANSI(out), "hello") {
		t.Fatalf("missing content: %q", stripANSI(out))
	}
}

func TestDialogFooterHint(t *testing.T) {
	d := NewDialog(styles.Chrome{})
	ink := styles.PlainOverlayInk()
	foot := d.RenderFooter(DialogFooter{
		HintLabel: "Remove",
		HintKey:   "ctrl+x",
	}, ink)
	plain := stripANSI(foot)
	if !strings.Contains(plain, "Remove") || !strings.Contains(plain, "ctrl+x") {
		t.Fatalf("missing hint: %q", plain)
	}
	panel := d.PanelWithFooter("body", DialogFooter{HintLabel: "Remove", HintKey: "ctrl+x"}, 40, ink)
	if !strings.Contains(stripANSI(panel), "body") {
		t.Fatalf("missing body: %q", stripANSI(panel))
	}
}
