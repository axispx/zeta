package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/axispx/zeta/internal/core"
)

const (
	usageNoneText = "No usage reported yet"
	usageLabelW   = 13 // label column width so values line up ("cached input" is 12)
)

// renderUsage is the /usage transcript block for a session's token accounting.
func renderUsage(u core.Usage) string {
	n := strconv.Itoa(u.Responses)
	label := "responses"
	if u.Responses == 1 {
		label = "response"
	}
	lines := []string{
		"Usage · " + n + " " + label,
		usageLine("input", formatTokenCount(u.Input)),
		usageLine("output", formatTokenCount(u.Output)),
		usageLine("total", formatTokenCount(u.Total)),
	}
	if u.CacheReported {
		lines = append(lines, usageLine("cached input", cachedValue(u.Cached, u.Input)))
	}
	if u.CacheWrite > 0 {
		lines = append(lines, usageLine("cache write", formatTokenCount(u.CacheWrite)))
	}
	// A single-model session needs no breakdown; the totals are the breakdown.
	if len(u.Models) > 1 {
		lines = append(lines, "By model:")
		for _, m := range u.Models {
			lines = append(lines, "  "+modelUsageLine(m))
		}
	}
	return strings.Join(lines, "\n")
}

// modelUsageLine is "display name  ·  N in · N out [· N cached]".
func modelUsageLine(m *core.ModelUsage) string {
	name := m.Name
	if name == "" {
		name = "unknown"
	}
	parts := []string{
		formatTokenCount(m.Input) + " in",
		formatTokenCount(m.Output) + " out",
		"total " + formatTokenCount(m.Total),
	}
	if m.Cached > 0 {
		parts = append(parts, formatTokenCount(m.Cached)+" cached")
	}
	return name + " · " + strings.Join(parts, " · ")
}

// usageLine is a "  label        value" row.
func usageLine(label, value string) string {
	return "  " + fmt.Sprintf("%-*s", usageLabelW, label) + value
}

// cachedValue is the cached prompt total with its share of input, e.g.
// "96.0k (80%)". The percent is dropped when input is unknown.
//
// Providers fold cached tokens into their prompt count, so the share is of the
// prompt as the provider reported it.
func cachedValue(cached, input int64) string {
	if input <= 0 {
		return formatTokenCount(cached)
	}
	return formatTokenCount(cached) + " (" + strconv.Itoa(int(cached*100/input)) + "%)"
}

// reportUsage handles /usage: cumulative token accounting for this session.
func (m *Model) reportUsage() {
	if m.Usage.Empty() {
		m.noteSystem(usageNoneText)
		return
	}
	m.noteSystem(renderUsage(m.Usage))
}
