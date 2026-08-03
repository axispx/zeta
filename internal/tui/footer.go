package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/workspace"
)

// footerRows is the fixed height of the input footer (usage/model + path/stats).
const footerRows = 2

// inputFooter is two rows under the input box:
//
//	model · % · tokens                              mode
//	cwd · branch                                   +N -M
func inputFooter(width int, ws workspace.Context, cfg config.Config, mode prompt.Mode, contextTokens int64, diff lineStats) string {
	if width < 1 {
		return ""
	}
	top := footerTopRow(width, cfg, mode, contextTokens)
	bot := footerBottomRow(width, ws, diff)
	return lipgloss.JoinVertical(lipgloss.Left, top, bot)
}

// footerTopRow is model · % · tokens (left) and mode (right).
func footerTopRow(width int, cfg config.Config, mode prompt.Mode, contextTokens int64) string {
	right := modeStyle(mode).Render(mode.Label())
	leftMax := footerLeftBudget(width, right)
	left := footerUsageModel(contextTokens, cfg.ContextWindow(), cfg.ModelName(), leftMax)
	return footerSplitRow(width, left, right)
}

// footerBottomRow is path · branch (left) and +N -M (right).
func footerBottomRow(width int, ws workspace.Context, diff lineStats) string {
	right := formatDiffStats(diff)
	leftMax := footerLeftBudget(width, right)
	left := styles.SystemMsg.Render(footerPathLabel(ws.Cwd, ws.Branch, leftMax))
	return footerSplitRow(width, left, right)
}

// footerLeftBudget is width minus right, or 0 if right alone fills the row.
// Callers fit left into this budget before footerSplitRow.
func footerLeftBudget(width int, right string) int {
	if right == "" {
		return width
	}
	rw := lipgloss.Width(right)
	if rw <= 0 {
		return width
	}
	if rw >= width {
		return 0
	}
	return width - rw
}

// footerSplitRow pins right and fills the rest with left + gap.
// left should already fit footerLeftBudget(width, right); overflow is a safety net.
func footerSplitRow(width int, left, right string) string {
	if width < 1 {
		return ""
	}
	if right == "" {
		return left
	}
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	if lw+rw > width {
		if lw > 0 {
			return left
		}
		return right
	}
	if lw == 0 {
		return lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Render(right)
	}
	gapW := width - lw
	return lipgloss.JoinHorizontal(lipgloss.Top,
		left,
		lipgloss.Place(gapW, 1, lipgloss.Right, lipgloss.Top, right),
	)
}

// footerUsageModel is "model · % · tokens" (tokens/% omitted when unknown),
// truncated on the right to maxW.
func footerUsageModel(contextTokens int64, contextWindow int, model string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	if model != "" {
		parts = append(parts, model)
	}
	if u := formatUsage(contextTokens, contextWindow); u != "" {
		parts = append(parts, u)
	}
	if len(parts) == 0 {
		return ""
	}
	return styles.SystemMsg.Render(truncateRight(strings.Join(parts, " · "), maxW))
}

// footerPathLabel is "cwd · branch" fitted into maxW.
//
//  1. full path · branch when it fits
//  2. shorten path from the left (…/tail) keeping branch
//  3. path only (shortened), then hard left-ellipsis
func footerPathLabel(cwd, branch string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if branch == "" {
		return shortenPath(cwd, maxW)
	}
	const sep = " · "
	full := cwd + sep + branch
	if lipgloss.Width(full) <= maxW {
		return full
	}
	// Reserve branch on the right; shrink path.
	suffix := sep + branch
	sw := lipgloss.Width(suffix)
	if sw < maxW {
		p := shortenPath(cwd, maxW-sw)
		if p != "" {
			return p + suffix
		}
	}
	// Branch alone if it fits; otherwise path-only / hard truncate.
	if lipgloss.Width(branch) <= maxW {
		return branch
	}
	return shortenPath(cwd, maxW)
}

// shortenPath fits a display path into maxW cells.
// Prefers dropping leading directories (…/b/c) over chopping the leaf name.
func shortenPath(path string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(path) <= maxW {
		return path
	}
	parts := strings.Split(path, "/")
	// Drop leading segments until the …/tail fits (keep at least the leaf).
	for n := len(parts) - 1; n >= 1; n-- {
		cand := "…/" + strings.Join(parts[len(parts)-n:], "/")
		if lipgloss.Width(cand) <= maxW {
			return cand
		}
	}
	// Single segment still too long (or path had no slash).
	leaf := parts[len(parts)-1]
	if leaf == "" {
		leaf = path
	}
	return truncateLeft(leaf, maxW)
}

// formatDiffStats is green +N / red -M; omits zero sides; empty when both zero.
func formatDiffStats(d lineStats) string {
	if d.empty() {
		return ""
	}
	if d.added > 0 && d.deleted > 0 {
		return styles.DiffAdd.Render("+"+strconv.Itoa(d.added)) +
			styles.SystemMsg.Render(" ") +
			styles.DiffDel.Render("-"+strconv.Itoa(d.deleted))
	}
	if d.added > 0 {
		return styles.DiffAdd.Render("+" + strconv.Itoa(d.added))
	}
	return styles.DiffDel.Render("-" + strconv.Itoa(d.deleted))
}

// formatUsage formats fill % then last-response context footprint (prompt+completion).
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
		return strconv.Itoa(pct) + "% · " + tok
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
