package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/workspace"
)

// inputFooter is cwd · branch (left) + mode · model (right) under the input box.
func inputFooter(width int, ws workspace.Context, cfg config.Config, mode prompt.Mode) string {
	if width < 1 {
		return ""
	}
	right := lipgloss.JoinHorizontal(lipgloss.Top,
		modeStyle(mode).Render(mode.Label()),
		styles.SystemMsg.Render(" · "+cfg.ModelName()),
	)
	rw := lipgloss.Width(right)
	if rw >= width {
		return lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Render(right)
	}

	maxLeft := width - rw
	leftText := ws.Label()
	left := styles.SystemMsg.Render(truncateLeft(leftText, maxLeft))
	gapW := width - lipgloss.Width(left)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		left,
		lipgloss.Place(gapW, 1, lipgloss.Right, lipgloss.Top, right),
	)
}

func modeStyle(m prompt.Mode) lipgloss.Style {
	switch m {
	case prompt.ModeAsk:
		return styles.StyleModeAsk
	case prompt.ModePlan:
		return styles.StyleModePlan
	default:
		return styles.StyleModeBuild
	}
}

// truncateLeft shortens s to at most maxW display cells, prefixing with … if needed.
func truncateLeft(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW <= 1 {
		return "…"
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		cand := "…" + string(runes[i:])
		if lipgloss.Width(cand) <= maxW {
			return cand
		}
	}
	return "…"
}

// truncateRight shortens s to at most maxW display cells, suffixing with … if needed.
func truncateRight(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW <= 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > maxW {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
