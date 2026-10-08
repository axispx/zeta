package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/workspace"
)

// footerRows is the fixed height of the input footer (path left, model and usage right).
const footerRows = 1

var (
	footerPathStyle   = lipgloss.NewStyle().Foreground(styles.Green)
	footerBranchStyle = lipgloss.NewStyle().Foreground(styles.Red)
	footerModelStyle  = lipgloss.NewStyle().Foreground(styles.Blue)
)

// inputFooter is one row under the input box:
//
//	cwd · branch                      model effort · %
func inputFooter(width int, ws workspace.Context, cfg config.Config, contextTokens int64) string {
	if width < 1 {
		return ""
	}
	model := footerModelLabel(cfg.ModelName(), cfg.ActiveReasoningEffort(), cfg.ActiveFast())
	if u := formatUsage(contextTokens, cfg.ContextWindow()); u != "" && model != "" {
		model += " · " + u
	}
	right := ""
	if model != "" {
		right = footerModelStyle.Render(truncateRight(model, width))
	}
	leftMax := width
	if rw := lipgloss.Width(right); rw > 0 {
		leftMax = max(width-rw-1, 0)
	}
	left := footerPathColored(ws.Cwd, ws.Branch, leftMax)
	top := left
	if gap := width - lipgloss.Width(left) - lipgloss.Width(right); right != "" && gap >= 0 {
		top = left + strings.Repeat(" ", gap) + right
	}
	return top
}

// footerModelLabel is "model effort Fast": effort and the Fast marker are
// omitted when unknown or off.
func footerModelLabel(model, effort string, fast bool) string {
	if model == "" {
		return ""
	}
	if effort != "" {
		model += " " + config.ReasoningEffortLabel(effort)
	}
	if fast {
		model += " Fast"
	}
	return model
}

// footerPathColored is footerPathLabel with the path green and branch red.
func footerPathColored(cwd, branch string, maxW int) string {
	label := footerPathLabel(cwd, branch, maxW)
	if label == "" {
		return ""
	}
	if branch == "" {
		return footerPathStyle.Render(label)
	}
	if label == branch {
		return footerBranchStyle.Render(label)
	}
	const sep = " · "
	if path, ok := strings.CutSuffix(label, sep+branch); ok {
		return footerPathStyle.Render(path) + styles.SystemMsg.Render(sep) + footerBranchStyle.Render(branch)
	}
	return footerPathStyle.Render(label)
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

// formatUsage is the context fill percent, empty when tokens or the window are
// unknown.
func formatUsage(contextTokens int64, contextWindow int) string {
	if contextTokens <= 0 || contextWindow <= 0 {
		return ""
	}
	pct := int((contextTokens * 100) / int64(contextWindow))
	if pct < 1 {
		pct = 1
	}
	return strconv.Itoa(pct) + "%"
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
