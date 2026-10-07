package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/harness"
	"github.com/axispx/zeta/internal/session"
)

func TestSessionUsageRender(t *testing.T) {
	var u harness.Usage
	u.Add("Sonnet", ai.Usage{
		PromptTokens:     120_000,
		CompletionTokens: 8_400,
		TotalTokens:      128_400,
		CachedTokens:     96_000,
		CacheReported:    true,
	})
	out := renderUsage(u, nil, harness.Billing{Provider: "Anthropic", Plan: false})
	for _, want := range []string{
		"Usage · 1 response",
		"input", "120.0k",
		"output", "8.4k",
		"total", "128.4k",
		"cached input", "96.0k (80%)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
	// One model: the totals are the breakdown, so no per-model block.
	if strings.Contains(out, "By model:") {
		t.Fatalf("single model needs no breakdown:\n%s", out)
	}
	// No cache accounting reported: the cached row is hidden, not shown as 0%.
	var cold harness.Usage
	cold.Add("Sonnet", ai.Usage{PromptTokens: 100, CompletionTokens: 5})
	if out := renderUsage(cold, nil, harness.Billing{}); strings.Contains(out, "cached") {
		t.Fatalf("unreported cache must be hidden:\n%s", out)
	}
}

func TestSessionUsageRenderByModel(t *testing.T) {
	var u harness.Usage
	u.Add("Sonnet", ai.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, CachedTokens: 900, CacheReported: true})
	u.Add("Grok", ai.Usage{PromptTokens: 2000, CompletionTokens: 200, TotalTokens: 2200, CacheReported: true})

	out := renderUsage(u, nil, harness.Billing{Provider: "Anthropic", Plan: false})
	if !strings.Contains(out, "By model:") {
		t.Fatalf("multi-model session needs a breakdown:\n%s", out)
	}
	for _, want := range []string{"Sonnet · 1.0k in · 100 out · total 1.1k · 900 cached", "Grok · 2.0k in · 200 out · total 2.2k"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
	// Unknown attribution is labelled, not blank.
	var unknown harness.Usage
	unknown.Add("", ai.Usage{PromptTokens: 10, CompletionTokens: 1})
	unknown.Add("M", ai.Usage{PromptTokens: 10, CompletionTokens: 1})
	if out := renderUsage(unknown, nil, harness.Billing{Provider: "Anthropic"}); !strings.Contains(out, "unknown ·") {
		t.Fatalf("unattributed bucket must be labelled:\n%s", out)
	}
}

func TestSessionUsageRenderCacheWrite(t *testing.T) {
	var u harness.Usage
	u.Add("M", ai.Usage{PromptTokens: 1000, CompletionTokens: 100, CacheWriteTokens: 900, CacheReported: true})
	out := renderUsage(u, nil, harness.Billing{Provider: "Anthropic", Plan: false})
	if !strings.Contains(out, "cache write") || !strings.Contains(out, "900") {
		t.Fatalf("write side missing:\n%s", out)
	}
}

func TestReportUsageNotesTranscript(t *testing.T) {
	m := testModel()
	m.reportUsage()
	if last := m.transcript.messages[len(m.transcript.messages)-1]; last.Role != RoleSystem || last.Text != usageNoneText {
		t.Fatalf("empty session should say so: %+v", last)
	}

	m.session.Usage.Add("M", ai.Usage{PromptTokens: 100, CompletionTokens: 10})
	n := len(m.transcript.messages)
	m.reportUsage()
	if len(m.transcript.messages) != n+1 {
		t.Fatalf("messages = %d, want %d", len(m.transcript.messages), n+1)
	}
	if got := m.transcript.messages[len(m.transcript.messages)-1].Text; !strings.Contains(got, "Usage · 1 response") {
		t.Fatalf("report = %q", got)
	}
}

func TestUsageBillingNote(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		billing harness.Billing
		want    string
	}{
		{"api key", harness.Billing{Provider: "DeepSeek"}, "API pricing"},
		{"subscription", harness.Billing{Provider: "OpenAI", Plan: true}, "OpenAI subscription"},
		{"subscription without a label", harness.Billing{Plan: true}, "provider subscription"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := usageBillingNote(tc.billing); !strings.Contains(got, tc.want) {
				t.Fatalf("note = %q, want %q in it", got, tc.want)
			}
		})
	}
}

func TestPlanLines(t *testing.T) {
	t.Parallel()
	// Nothing reported is nothing shown: a zeroed gauge would read as 0% used.
	if got := planLines(nil); len(got) != 0 {
		t.Fatalf("empty plan = %#v", got)
	}
	if got := planLines(&codex.PlanUsage{}); len(got) != 0 {
		t.Fatalf("empty plan = %#v", got)
	}

	reset := time.Now().Add(2*time.Hour + 14*time.Minute).Unix()
	plan := &codex.PlanUsage{
		Primary:   &codex.Window{UsedPercent: 12.5, WindowMinutes: 300, ResetsAt: reset},
		Secondary: &codex.Window{UsedPercent: 80, WindowMinutes: 10080, ResetsAt: time.Now().Add(4*24*time.Hour + 19*time.Hour + 30*time.Second).Unix()},
		Credits:   &codex.Credits{HasCredits: true, Balance: "$5.00"},
	}
	got := strings.Join(planLines(plan), "\n")
	for _, want := range []string{"Plan quota", "5h window", "12.5% used", "weekly", "80% used", "resets", "2h 14m", "weekly resets", "4d 19h", "$5.00"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestWindowLabelAndCredits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		minutes int64
		want    string
	}{
		{0, "window"}, // a plan that disabled the window reports no length
		{300, "5h window"},
		{60, "1h window"},
		{10080, "weekly"},
		{2880, "2d window"},
		{90, "90m window"},
	}
	for _, tc := range cases {
		if got := windowLabel(&codex.Window{WindowMinutes: tc.minutes}); got != tc.want {
			t.Fatalf("windowLabel(%d) = %q, want %q", tc.minutes, got, tc.want)
		}
	}
	credits := []struct {
		c    codex.Credits
		want string
	}{
		{codex.Credits{Unlimited: true}, "unlimited"},
		{codex.Credits{}, "none"},
		{codex.Credits{HasCredits: true, Balance: "$5.00"}, "$5.00"},
		{codex.Credits{HasCredits: true}, "available"},
	}
	for _, tc := range credits {
		if got := creditsValue(&tc.c); got != tc.want {
			t.Fatalf("creditsValue(%#v) = %q, want %q", tc.c, got, tc.want)
		}
	}
	now := time.Now()
	countdowns := []struct {
		in   time.Duration
		want string
	}{
		{4*24*time.Hour + 19*time.Hour + 20*time.Minute, "4d 19h"},
		{2 * 24 * time.Hour, "2d"},
		{24*time.Hour + 5*time.Minute, "1d"},
		{2*time.Hour + 34*time.Minute, "2h 34m"},
		{3 * time.Hour, "3h"},
		{12 * time.Minute, "12m"},
	}
	for _, tc := range countdowns {
		if got := resetCountdown(now.Add(tc.in).Unix(), now); got != tc.want {
			t.Fatalf("resetCountdown(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// A window that already rolled over reads as "now", not a negative count.
	if got := resetCountdown(time.Now().Add(-time.Minute).Unix(), time.Now()); got != "now" {
		t.Fatalf("past reset = %q", got)
	}
}

// /usage must not block the UI on a quota round trip: it writes the token block
// and returns a cmd only when a provider reports a plan.
func TestReportUsageFetchesPlanOnlyForPlans(t *testing.T) {
	isolateZetaHome(t)

	// A per-token provider has no quota, so /usage is synchronous.
	m := testModel()
	m.session.Cfg = config.Config{
		Active: "x/m",
		Providers: map[string]config.Provider{
			"x": {BaseURL: "https://api.x.ai/v1", APIKey: "k", Models: map[string]config.ModelDef{"m": {ContextWindow: 1000}}},
		},
	}
	m.session.Usage.Add("M", ai.Usage{PromptTokens: 10, CompletionTokens: 1})
	if cmd := m.reportUsage(); cmd != nil {
		t.Fatal("an api-key provider must not trigger a quota fetch")
	}
	last := m.transcript.messages[len(m.transcript.messages)-1]
	if !strings.Contains(last.Text, "API pricing") {
		t.Fatalf("billing note missing:\n%s", last.Text)
	}

	// A ChatGPT plan reports quota, so /usage returns the fetch cmd.
	m = testModel()
	m.session.Cfg = config.Config{
		Active: "openai/m",
		Providers: map[string]config.Provider{
			"openai": {
				BaseURL: codex.BaseURL,
				OAuth:   &config.OAuthCredential{AccessToken: "at", AccountID: "acct"},
				Models:  map[string]config.ModelDef{"m": {ContextWindow: 1000}},
			},
		},
	}
	m.session.Usage.Add("OpenAI M", ai.Usage{PromptTokens: 10, CompletionTokens: 1})
	cmd := m.reportUsage()
	if cmd == nil {
		t.Fatal("a codex provider must offer to fetch its quota")
	}
	last = m.transcript.messages[len(m.transcript.messages)-1]
	if !strings.Contains(last.Text, "subscription") {
		t.Fatalf("billing note missing:\n%s", last.Text)
	}

	// Once the quota is known the plan renders in place, with no fetch.
	m.session.Plan = &codex.PlanUsage{Primary: &codex.Window{UsedPercent: 50, WindowMinutes: 300}}
	n := len(m.transcript.messages)
	if cmd := m.reportUsage(); cmd != nil {
		t.Fatal("a known quota needs no fetch")
	}
	if len(m.transcript.messages) != n+1 {
		t.Fatalf("messages = %d, want %d", len(m.transcript.messages), n+1)
	}
	if got := m.transcript.messages[len(m.transcript.messages)-1].Text; !strings.Contains(got, "Plan quota") || !strings.Contains(got, "50% used") {
		t.Fatalf("plan missing from report:\n%s", got)
	}
}

func TestHandlePlanUsage(t *testing.T) {
	isolateZetaHome(t)
	m := testModel()
	m.session.Usage.Add("M", ai.Usage{PromptTokens: 10, CompletionTokens: 1})

	// A failed fetch is reported, but the session keeps the numbers it has.
	m.handlePlanUsage(planUsageMsg{err: errors.New("boom")})
	last := m.transcript.messages[len(m.transcript.messages)-1]
	if last.Role != RoleSystem || !strings.Contains(last.Text, "boom") {
		t.Fatalf("error note = %#v", last)
	}
	if !m.session.Plan.Empty() {
		t.Fatalf("a failed fetch must not install a plan: %#v", m.session.Plan)
	}

	// An empty result is silent: the provider simply has no quota to show.
	n := len(m.transcript.messages)
	m.handlePlanUsage(planUsageMsg{plan: &codex.PlanUsage{}})
	if len(m.transcript.messages) != n {
		t.Fatal("an empty plan must not note anything")
	}

	// A real quota is installed and printed.
	m.handlePlanUsage(planUsageMsg{plan: &codex.PlanUsage{Primary: &codex.Window{UsedPercent: 90, WindowMinutes: 10080}}})
	if m.session.Plan.Primary.UsedPercent != 90 {
		t.Fatalf("plan not installed: %#v", m.session.Plan)
	}
	if got := m.transcript.messages[len(m.transcript.messages)-1].Text; !strings.Contains(got, "90% used") {
		t.Fatalf("plan not reported:\n%s", got)
	}
}

// handleTurnAssistant is the live path: it totals in memory and writes the same
// numbers to the transcript record, so /usage and a later /resume agree.
func TestHandleTurnAssistantPersistsUsage(t *testing.T) {
	isolateZetaHome(t)
	proj := t.TempDir()
	sess, err := session.New(proj)
	if err != nil {
		t.Fatal(err)
	}
	m := testModel()
	m.session.Log = sess
	m.session.Cfg = testFooterCfg()
	m.turn.current = &turnSession{activeTool: -1}

	usage := ai.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		TotalTokens:      1200,
		CachedTokens:     900,
		CacheReported:    true,
	}
	msg := turnAssistantMsg{
		message: ai.Message{Role: ai.RoleAssistant, Text: "hi"},
		usage:   usage,
	}
	if cmd := m.handleTurnAssistant(msg); cmd == nil {
		t.Fatal("expected a wait command")
	}
	if m.session.Usage.Responses != 1 || m.session.Usage.Total != 1200 {
		t.Fatalf("live usage = %+v", m.session.Usage)
	}
	if m.session.ContextTokens != 1200 {
		t.Fatalf("contextTokens = %d", m.session.ContextTokens)
	}

	_, recs, err := session.OpenID(proj, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Usage == nil {
		t.Fatalf("records = %+v", recs)
	}
	if *recs[0].Usage != usage {
		t.Fatalf("persisted usage = %+v, want %+v", *recs[0].Usage, usage)
	}
	if recs[0].Model != m.session.Cfg.ModelName() {
		t.Fatalf("persisted model = %q, want %q", recs[0].Model, m.session.Cfg.ModelName())
	}
	if got := harness.UsageFromRecords(recs); got.Total != m.session.Usage.Total || got.Input != m.session.Usage.Input {
		t.Fatalf("resumed totals = %+v, want %+v", got, m.session.Usage)
	}
}

// A resumed session starts with the totals already accumulated, and /clear
// drops them.
func TestApplySessionSeedsAndClearsUsage(t *testing.T) {
	m := testModel()
	recs := []session.Record{
		{Role: session.RoleAgent, Text: "a", Model: "M1", Usage: &ai.Usage{PromptTokens: 100, CompletionTokens: 10}},
	}
	m.session.Usage.Add("M0", ai.Usage{PromptTokens: 9, CompletionTokens: 1})
	m.applySession(nil, recs, nil)
	if m.session.Usage.Responses != 1 || m.session.Usage.Total != 110 {
		t.Fatalf("resume totals = %+v", m.session.Usage)
	}
	m.applySession(nil, nil, nil)
	if !m.session.Usage.Empty() {
		t.Fatalf("new session must zero usage: %+v", m.session.Usage)
	}
}
