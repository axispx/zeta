package tui

import (
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/session"
)

func TestSessionUsageAddSkipsUnreported(t *testing.T) {
	var u sessionUsage
	u.add("m", ai.Usage{})
	if !u.empty() || u.responses != 0 {
		t.Fatalf("empty usage must not count a turn: %+v", u)
	}
	if len(u.models) != 0 {
		t.Fatalf("unreported turn must not create a bucket: %+v", u.models)
	}
	u.add("m", ai.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120})
	if u.empty() || u.responses != 1 {
		t.Fatalf("reported usage must count: %+v", u)
	}
}

func TestSessionUsageTotals(t *testing.T) {
	var u sessionUsage
	u.add("M1", ai.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		TotalTokens:      1200,
		CachedTokens:     800,
		CacheReported:    true,
	})
	// A provider that omits TotalTokens falls back to prompt+completion.
	u.add("M1", ai.Usage{
		PromptTokens:     500,
		CompletionTokens: 100,
		CachedTokens:     100,
		CacheWriteTokens: 50,
		CacheReported:    true,
	})
	if u.responses != 2 {
		t.Fatalf("responses = %d", u.responses)
	}
	if u.input != 1500 || u.output != 300 {
		t.Fatalf("input/output = %d/%d, want 1500/300", u.input, u.output)
	}
	if u.total != 1800 {
		t.Fatalf("total = %d, want 1200+600", u.total)
	}
	if u.cached != 900 || !u.cacheReported {
		t.Fatalf("cached = %d reported = %v", u.cached, u.cacheReported)
	}
	if u.cacheWrite != 50 {
		t.Fatalf("cacheWrite = %d", u.cacheWrite)
	}
	if len(u.models) != 1 || u.models[0].total != 1800 {
		t.Fatalf("model bucket = %+v", u.models)
	}
}

// Switching models mid-session must not reset the total: every turn was billed.
// It does split the accounting, because cache numbers are per-model.
func TestSessionUsageAccumulatesAcrossModelSwitch(t *testing.T) {
	var u sessionUsage
	u.add("Model A", ai.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, CachedTokens: 800, CacheReported: true})
	u.add("Model B", ai.Usage{PromptTokens: 2000, CompletionTokens: 300, TotalTokens: 2300, CachedTokens: 0, CacheReported: true})

	if u.responses != 2 || u.total != 3400 || u.input != 3000 {
		t.Fatalf("switch must not drop spend: %+v", u)
	}
	if len(u.models) != 2 {
		t.Fatalf("want one bucket per model: %+v", u.models)
	}
	if u.models[0].name != "Model A" || u.models[1].name != "Model B" {
		t.Fatalf("order must be first-seen: %+v", u.models)
	}
	if u.models[0].cached != 800 || u.models[1].cached != 0 {
		t.Fatalf("cache must stay per-model: %+v", u.models)
	}
	if u.models[0].total+u.models[1].total != u.total {
		t.Fatalf("buckets must sum to the total: %+v", u)
	}
}

func TestSessionUsageFromRecords(t *testing.T) {
	recs := []session.Record{
		{Role: session.RoleUser, Text: "hi"},
		{Role: session.RoleAgent, Text: "a", Model: "M1", Usage: &ai.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}},
		{Role: session.RoleTool, Text: "out"},
		{Role: session.RoleAgent, Text: "b", Model: "M1", Usage: &ai.Usage{PromptTokens: 200, CompletionTokens: 20, TotalTokens: 220}},
		{Role: session.RoleCompact, Text: "summary"},
		{Role: session.RoleAgent, Text: "c", Model: "M2", Usage: &ai.Usage{PromptTokens: 300, CompletionTokens: 30, TotalTokens: 330}},
		{Role: session.RoleAgent, Text: "d"}, // provider reported nothing
	}
	got := sessionUsageFrom(recs)
	if got.responses != 3 || got.input != 600 || got.total != 660 {
		t.Fatalf("totals from records = %+v", got)
	}
	byModel := map[string]int64{}
	for _, m := range got.models {
		byModel[m.name] = m.total
	}
	if byModel["M1"] != 330 || byModel["M2"] != 330 {
		t.Fatalf("per-model totals = %+v", byModel)
	}
}

func TestSessionUsageRender(t *testing.T) {
	var u sessionUsage
	u.add("Sonnet", ai.Usage{
		PromptTokens:     120_000,
		CompletionTokens: 8_400,
		TotalTokens:      128_400,
		CachedTokens:     96_000,
		CacheReported:    true,
	})
	out := u.render()
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
	var cold sessionUsage
	cold.add("Sonnet", ai.Usage{PromptTokens: 100, CompletionTokens: 5})
	if out := cold.render(); strings.Contains(out, "cached") {
		t.Fatalf("unreported cache must be hidden:\n%s", out)
	}
}

func TestSessionUsageRenderByModel(t *testing.T) {
	var u sessionUsage
	u.add("Sonnet", ai.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, CachedTokens: 900, CacheReported: true})
	u.add("Grok", ai.Usage{PromptTokens: 2000, CompletionTokens: 200, TotalTokens: 2200, CacheReported: true})

	out := u.render()
	if !strings.Contains(out, "By model:") {
		t.Fatalf("multi-model session needs a breakdown:\n%s", out)
	}
	for _, want := range []string{"Sonnet · 1.0k in · 100 out · total 1.1k · 900 cached", "Grok · 2.0k in · 200 out · total 2.2k"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
	// Unknown attribution is labelled, not blank.
	var unknown sessionUsage
	unknown.add("", ai.Usage{PromptTokens: 10, CompletionTokens: 1})
	unknown.add("M", ai.Usage{PromptTokens: 10, CompletionTokens: 1})
	if out := unknown.render(); !strings.Contains(out, "unknown ·") {
		t.Fatalf("unattributed bucket must be labelled:\n%s", out)
	}
}

func TestSessionUsageRenderCacheWrite(t *testing.T) {
	var u sessionUsage
	u.add("M", ai.Usage{PromptTokens: 1000, CompletionTokens: 100, CacheWriteTokens: 900, CacheReported: true})
	out := u.render()
	if !strings.Contains(out, "cache write") || !strings.Contains(out, "900") {
		t.Fatalf("write side missing:\n%s", out)
	}
}

func TestReportUsageNotesTranscript(t *testing.T) {
	m := testModel()
	m.reportUsage()
	if last := m.messages[len(m.messages)-1]; last.Role != RoleSystem || last.Text != usageNoneText {
		t.Fatalf("empty session should say so: %+v", last)
	}

	m.usage.add("M", ai.Usage{PromptTokens: 100, CompletionTokens: 10})
	n := len(m.messages)
	m.reportUsage()
	if len(m.messages) != n+1 {
		t.Fatalf("messages = %d, want %d", len(m.messages), n+1)
	}
	if got := m.messages[len(m.messages)-1].Text; !strings.Contains(got, "Usage · 1 response") {
		t.Fatalf("report = %q", got)
	}
}

func TestUsageOrNil(t *testing.T) {
	if got := usageOrNil(ai.Usage{}); got != nil {
		t.Fatalf("unreported usage must not persist: %+v", got)
	}
	got := usageOrNil(ai.Usage{PromptTokens: 1, CompletionTokens: 1})
	if got == nil || got.PromptTokens != 1 {
		t.Fatalf("got %+v", got)
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
	m.sess = sess
	m.cfg = testFooterCfg()
	m.turn = &turnSession{activeTool: -1}

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
	if m.usage.responses != 1 || m.usage.total != 1200 {
		t.Fatalf("live usage = %+v", m.usage)
	}
	if m.contextTokens != 1200 {
		t.Fatalf("contextTokens = %d", m.contextTokens)
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
	if recs[0].Model != m.cfg.ModelName() {
		t.Fatalf("persisted model = %q, want %q", recs[0].Model, m.cfg.ModelName())
	}
	if got := sessionUsageFrom(recs); got.total != m.usage.total || got.input != m.usage.input {
		t.Fatalf("resumed totals = %+v, want %+v", got, m.usage)
	}
}

// A resumed session starts with the totals already accumulated, and /clear
// drops them.
func TestApplySessionSeedsAndClearsUsage(t *testing.T) {
	m := testModel()
	recs := []session.Record{
		{Role: session.RoleAgent, Text: "a", Model: "M1", Usage: &ai.Usage{PromptTokens: 100, CompletionTokens: 10}},
	}
	m.usage.add("M0", ai.Usage{PromptTokens: 9, CompletionTokens: 1})
	m.applySession(nil, recs, nil)
	if m.usage.responses != 1 || m.usage.total != 110 {
		t.Fatalf("resume totals = %+v", m.usage)
	}
	m.applySession(nil, nil, nil)
	if !m.usage.empty() {
		t.Fatalf("new session must zero usage: %+v", m.usage)
	}
}
