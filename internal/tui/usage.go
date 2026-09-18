package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/session"
)

const (
	usageNoneText = "No usage reported yet"
	usageLabelW   = 13 // label column width so values line up ("cached input" is 12)
)

// modelUsage is one model's share of a session's token accounting.
type modelUsage struct {
	name       string // display name; "" when the provider/model is unknown
	input      int64
	output     int64
	total      int64
	cached     int64
	cacheWrite int64
	responses  int
}

// sessionUsage is provider-reported token accounting totalled over a session's
// assistant turns, for /usage.
//
// It is deliberately cumulative across /model switches rather than reset: every
// turn was billed, so dropping a switch's spend would under-report the session.
// Turns are bucketed by the model that produced them, which is also the only
// honest way to read cache numbers — each model has its own prompt cache, so a
// hit rate carried across a switch describes a cache that no longer exists.
// The footer's contextTokens reset on switch for that reason; this does not.
//
// It is also not the footer's context footprint: that is a single response's
// fill toward the model window, while this accumulates every billed call and
// survives /compact (compaction rewrites history but not the transcript).
// Summarizer and title calls are not counted — those completers report no
// usage.
type sessionUsage struct {
	models        []*modelUsage // first-seen order
	byName        map[string]*modelUsage
	input         int64
	output        int64
	total         int64
	cached        int64
	cacheWrite    int64
	responses     int
	cacheReported bool // at least one turn reported cache accounting
}

// bucket returns (creating if needed) the per-model bucket for name.
func (u *sessionUsage) bucket(name string) *modelUsage {
	if u.byName == nil {
		u.byName = make(map[string]*modelUsage, 2)
	}
	if m, ok := u.byName[name]; ok {
		return m
	}
	m := &modelUsage{name: name}
	u.byName[name] = m
	u.models = append(u.models, m)
	return m
}

// add folds one response's usage into the running totals, attributed to model.
// Responses with no token counts (providers that omit usage) are ignored rather
// than counted as a zero-token turn.
func (u *sessionUsage) add(model string, v ai.Usage) {
	total := v.ContextTokens()
	if total <= 0 {
		return
	}
	u.responses++
	u.input += v.PromptTokens
	u.output += v.CompletionTokens
	u.total += total
	u.cached += v.CachedTokens
	u.cacheWrite += v.CacheWriteTokens
	u.cacheReported = u.cacheReported || v.CacheReported

	m := u.bucket(model)
	m.responses++
	m.input += v.PromptTokens
	m.output += v.CompletionTokens
	m.total += total
	m.cached += v.CachedTokens
	m.cacheWrite += v.CacheWriteTokens
}

// empty reports whether nothing has been totalled yet.
func (u sessionUsage) empty() bool { return u.responses == 0 }

// usageOrNil returns a turn's accounting for persistence, or nil when the
// provider reported no token counts, so those records stay clean.
func usageOrNil(u ai.Usage) *ai.Usage {
	if u.ContextTokens() <= 0 {
		return nil
	}
	return &u
}

// sessionUsageFrom totals the usage persisted on a session's records, so a
// /resume shows the same numbers as the run that produced them.
func sessionUsageFrom(recs []session.Record) sessionUsage {
	var u sessionUsage
	for _, r := range recs {
		if r.Usage != nil {
			u.add(r.Model, *r.Usage)
		}
	}
	return u
}

// render is the /usage transcript block.
func (u sessionUsage) render() string {
	n := strconv.Itoa(u.responses)
	label := "responses"
	if u.responses == 1 {
		label = "response"
	}
	lines := []string{
		"Usage · " + n + " " + label,
		usageLine("input", formatTokenCount(u.input)),
		usageLine("output", formatTokenCount(u.output)),
		usageLine("total", formatTokenCount(u.total)),
	}
	if u.cacheReported {
		lines = append(lines, usageLine("cached input", cachedValue(u.cached, u.input)))
	}
	if u.cacheWrite > 0 {
		lines = append(lines, usageLine("cache write", formatTokenCount(u.cacheWrite)))
	}
	// A single-model session needs no breakdown; the totals are the breakdown.
	if len(u.models) > 1 {
		lines = append(lines, "By model:")
		for _, m := range u.models {
			lines = append(lines, "  "+modelUsageLine(m))
		}
	}
	return strings.Join(lines, "\n")
}

// modelUsageLine is "display name  ·  N in · N out [· N cached]".
func modelUsageLine(m *modelUsage) string {
	name := m.name
	if name == "" {
		name = "unknown"
	}
	parts := []string{
		formatTokenCount(m.input) + " in",
		formatTokenCount(m.output) + " out",
		"total " + formatTokenCount(m.total),
	}
	if m.cached > 0 {
		parts = append(parts, formatTokenCount(m.cached)+" cached")
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
	if m.usage.empty() {
		m.noteSystem(usageNoneText)
		return
	}
	m.noteSystem(m.usage.render())
}
