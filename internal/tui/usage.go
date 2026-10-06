package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/harness"
)

const (
	usageNoneText = "No usage reported yet"
	usageLabelW   = 13 // label column width so values line up ("cached input" is 12)
)

// renderUsage is the /usage transcript block for a session's token accounting.
// plan is the provider's subscription quota, or nil when it reported none.
func renderUsage(u harness.Usage, plan *codex.PlanUsage, billing harness.Billing) string {
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
	lines = append(lines, "", usageBillingNote(billing))
	lines = append(lines, planLines(plan)...)
	return strings.Join(lines, "\n")
}

// usageBillingNote says what the token totals were billed against. A ChatGPT
// plan or a Grok subscription meters by quota, so a token count is not a bill
// and reading it as API pricing would be wrong.
func usageBillingNote(b harness.Billing) string {
	if !b.Plan {
		return "Billed per token at your provider's API pricing."
	}
	who := b.Provider
	if who == "" {
		who = "provider"
	}
	return "Billed to your " + who + " subscription, not per-token API pricing."
}

// planLines renders the subscription quota. It is empty when the provider
// reported none: a plan only reports quota alongside a response, so silence is
// the honest output rather than a zeroed gauge.
func planLines(plan *codex.PlanUsage) []string {
	if plan.Empty() {
		return nil
	}
	out := []string{"Plan quota"}
	if w := plan.Primary; w != nil {
		out = append(out, usageLine(windowLabel(w), windowUsed(w)))
	}
	if w := plan.Secondary; w != nil {
		out = append(out, usageLine(windowLabel(w), windowUsed(w)))
	}
	if w := plan.Primary; w != nil && w.ResetsAt > 0 {
		out = append(out, usageLine("resets", resetCountdown(w.ResetsAt, time.Now())))
	}
	if c := plan.Credits; c != nil {
		out = append(out, usageLine("credits", creditsValue(c)))
	}
	return out
}

func windowUsed(w *codex.Window) string {
	return strconv.FormatFloat(w.UsedPercent, 'f', -1, 64) + "% used"
}

// windowLabel names a quota window by its length. The backend's windows are the
// rolling ~5h one and the weekly one; an unrecognized or disabled length keeps
// the raw position rather than inventing a period.
func windowLabel(w *codex.Window) string {
	m := w.WindowMinutes
	switch {
	case m <= 0:
		return "window"
	case m == 7*24*60:
		return "weekly"
	case m%1440 == 0:
		return strconv.FormatInt(m/1440, 10) + "d window"
	case m%60 == 0:
		return strconv.FormatInt(m/60, 10) + "h window"
	default:
		return strconv.FormatInt(m, 10) + "m window"
	}
}

// resetCountdown is how long until the window rolls over, e.g. "2h 14m".
func resetCountdown(resetsAt int64, now time.Time) string {
	d := time.Unix(resetsAt, 0).Sub(now).Round(time.Minute)
	if d <= 0 {
		return "now"
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h == 0:
		return strconv.Itoa(m) + "m"
	case m == 0:
		return strconv.Itoa(h) + "h"
	default:
		return fmt.Sprintf("%dh %dm", h, m)
	}
}

func creditsValue(c *codex.Credits) string {
	switch {
	case c.Unlimited:
		return "unlimited"
	case !c.HasCredits:
		return "none"
	case c.Balance != "":
		return c.Balance
	}
	return "available"
}

// modelUsageLine is "display name  ·  N in · N out [· N cached]".
func modelUsageLine(m *harness.ModelUsage) string {
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

// reportUsage handles /usage: cumulative token accounting for this session,
// plus the provider's plan quota when it meters by subscription.
//
// The quota may need a round trip — a plan that sends it only in response
// headers has none until a turn runs — so the token block is written first and
// the quota arrives as a second note.
func (m *Model) reportUsage() tea.Cmd {
	if m.session.Usage.Empty() {
		m.noteSystem(usageNoneText)
		return nil
	}
	m.noteSystem(renderUsage(m.session.Usage, m.session.Plan, harness.BillingOf(m.session.Cfg)))
	if m.session.Plan.Empty() && m.canFetchPlan() {
		return m.fetchPlanUsage()
	}
	return nil
}

// canFetchPlan reports whether the active provider has a quota to fetch.
func (m *Model) canFetchPlan() bool {
	_, ok := harness.SubscriptionPlan(m.session.Cfg)
	return ok
}

// planUsageMsg is the outcome of an on-demand quota fetch.
type planUsageMsg struct {
	plan *codex.PlanUsage
	err  error
}

// fetchPlanUsage asks the provider for the account's plan quota. The cmd works
// on a config clone: Bubble Tea's value-receiver Update must not share the live
// map with another goroutine.
func (m *Model) fetchPlanUsage() tea.Cmd {
	cfg := m.session.Cfg.Clone()
	return func() tea.Msg {
		plan, err := harness.PlanQuota(context.Background(), cfg)
		return planUsageMsg{plan: plan, err: err}
	}
}

// handlePlanUsage installs the fetched quota and prints it.
func (m *Model) handlePlanUsage(msg planUsageMsg) {
	if msg.err != nil {
		m.noteSystem("Plan quota unavailable: " + msg.err.Error())
		return
	}
	if msg.plan.Empty() {
		return
	}
	m.session.Plan = msg.plan
	if m.session.Usage.Empty() {
		return
	}
	m.noteSystem(renderUsage(m.session.Usage, m.session.Plan, harness.BillingOf(m.session.Cfg)))
}
