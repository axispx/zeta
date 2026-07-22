package tui

import (
	"fmt"
	"strconv"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/workspace"
)

// inputFooter is cwd · branch (left) + tokens · mode · model (right) under the input box.
func inputFooter(width int, ws workspace.Context, cfg config.Config, mode prompt.Mode, contextTokens int64) string {
	if width < 1 {
		return ""
	}
	parts := []string{}
	if usage := formatUsage(contextTokens, cfg.ContextWindow()); usage != "" {
		parts = append(parts, styles.SystemMsg.Render(usage+" · "))
	}
	parts = append(parts,
		modeStyle(mode).Render(mode.Label()),
		styles.SystemMsg.Render(" · "+cfg.ModelName()),
	)
	right := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
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

// formatUsage formats last-response context footprint (prompt+completion) and fill %.
func formatUsage(contextTokens int64, contextWindow int) string {
	if contextTokens <= 0 {
		return ""
	}
	tok := formatTokenCount(contextTokens)
	if contextWindow > 0 {
		pct := int((contextTokens * 100) / int64(contextWindow))
		if pct < 1 {
			pct = 1
		}
		return tok + " · " + strconv.Itoa(pct) + "%"
	}
	return tok
}

func formatTokenCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return strconv.FormatInt(n, 10)
	}
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
