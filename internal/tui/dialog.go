package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

// Dialog is a centered modal panel with a dim scrim behind it.
type Dialog struct {
	MaxWidth int
	MinWidth int
	// PanelBG fills the dialog chrome (border + padding). Nil keeps terminal default.
	PanelBG color.Color
	// ScrimBG fills the area around the panel. Nil skips the scrim (transparent place).
	ScrimBG color.Color
	// BorderFG defaults to styles.Dim.
	BorderFG color.Color
}

// DialogFooter is optional bottom chrome: hint text and/or HintLabel+HintKey.
type DialogFooter struct {
	// Hint is pre-rendered footer text (dim hints, custom markup).
	Hint string
	// HintLabel + HintKey render via OverlayInk.HintKbd when Hint is empty.
	HintLabel string
	HintKey   string
}

func (f DialogFooter) empty() bool {
	return f.Hint == "" && f.HintLabel == "" && f.HintKey == ""
}

const (
	dialogPadH        = 4 // Padding(1, 2, 0, 2) horizontal
	dialogBorderH     = 2 // rounded border L+R
	dialogDefaultMaxW = 64
	dialogDefaultMinW = 36
	dialogScrimDarken = 0.55
)

// NewDialog builds a dialog styled from chrome (panel lift + darkened scrim).
func NewDialog(chrome styles.Chrome) Dialog {
	d := Dialog{
		MaxWidth: dialogDefaultMaxW,
		MinWidth: dialogDefaultMinW,
		PanelBG:  chrome.Input,
		BorderFG: styles.Dim,
	}
	if chrome.Input != nil {
		d.ScrimBG = lipgloss.Darken(chrome.Input, dialogScrimDarken)
	} else {
		d.ScrimBG = lipgloss.Color("0")
	}
	return d
}

// FitWidth returns panel total width and inner content width for the terminal.
func (d Dialog) FitWidth(termW int) (panelW, contentW int) {
	maxW := d.MaxWidth
	if maxW < 1 {
		maxW = dialogDefaultMaxW
	}
	minW := d.MinWidth
	if minW < 1 {
		minW = dialogDefaultMinW
	}

	panelW = termW - 6
	if panelW > maxW {
		panelW = maxW
	}
	if panelW < minW {
		panelW = minW
	}
	if termW > 0 && panelW > termW {
		panelW = termW
		if panelW < 24 {
			panelW = 24
		}
	}
	contentW = panelW - dialogPadH - dialogBorderH
	if contentW < 16 {
		contentW = 16
		panelW = contentW + dialogPadH + dialogBorderH
	}
	return panelW, contentW
}

// RenderFooter builds the hint line for the dialog bottom.
func (d Dialog) RenderFooter(f DialogFooter, ink styles.OverlayInk) string {
	if f.empty() {
		return ""
	}
	if f.Hint != "" {
		return f.Hint
	}
	if f.HintKey != "" {
		return ink.HintKbd(f.HintLabel, f.HintKey)
	}
	return ink.HintText.Render(f.HintLabel)
}

// Panel renders content inside the bordered dialog box.
func (d Dialog) Panel(content string, panelW int) string {
	border := d.BorderFG
	if border == nil {
		border = styles.Dim
	}
	s := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(1, 2, 0, 2).
		Width(panelW)
	if d.PanelBG != nil {
		s = s.Background(d.PanelBG).BorderBackground(d.PanelBG)
	}
	return s.Render(content)
}

// PanelWithFooter renders body + footer inside the bordered dialog box.
func (d Dialog) PanelWithFooter(body string, footer DialogFooter, panelW int, ink styles.OverlayInk) string {
	content := body
	if foot := d.RenderFooter(footer, ink); foot != "" {
		if content != "" {
			content += "\n\n"
		}
		content += foot
	}
	return d.Panel(content, panelW)
}

// Place centers the panel in areaW×areaH with a dim scrim filling the rest.
func (d Dialog) Place(areaW, areaH int, panel string) string {
	if areaW < 1 {
		areaW = 1
	}
	if areaH < 1 {
		areaH = 1
	}
	var opts []lipgloss.WhitespaceOption
	if d.ScrimBG != nil {
		opts = append(opts, lipgloss.WithWhitespaceStyle(
			lipgloss.NewStyle().Background(d.ScrimBG),
		))
	}
	return lipgloss.Place(areaW, areaH, lipgloss.Center, lipgloss.Center, panel, opts...)
}
