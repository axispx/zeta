package tui

import (
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/core"
	"github.com/axispx/zeta/internal/session"
)

func TestSessionUsageRender(t *testing.T) {
	var u core.Usage
	u.Add("Sonnet", ai.Usage{
		PromptTokens:     120_000,
		CompletionTokens: 8_400,
		TotalTokens:      128_400,
		CachedTokens:     96_000,
		CacheReported:    true,
	})
	out := renderUsage(u)
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
	var cold core.Usage
	cold.Add("Sonnet", ai.Usage{PromptTokens: 100, CompletionTokens: 5})
	if out := renderUsage(cold); strings.Contains(out, "cached") {
		t.Fatalf("unreported cache must be hidden:\n%s", out)
	}
}

func TestSessionUsageRenderByModel(t *testing.T) {
	var u core.Usage
	u.Add("Sonnet", ai.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, CachedTokens: 900, CacheReported: true})
	u.Add("Grok", ai.Usage{PromptTokens: 2000, CompletionTokens: 200, TotalTokens: 2200, CacheReported: true})

	out := renderUsage(u)
	if !strings.Contains(out, "By model:") {
		t.Fatalf("multi-model session needs a breakdown:\n%s", out)
	}
	for _, want := range []string{"Sonnet · 1.0k in · 100 out · total 1.1k · 900 cached", "Grok · 2.0k in · 200 out · total 2.2k"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
	// Unknown attribution is labelled, not blank.
	var unknown core.Usage
	unknown.Add("", ai.Usage{PromptTokens: 10, CompletionTokens: 1})
	unknown.Add("M", ai.Usage{PromptTokens: 10, CompletionTokens: 1})
	if out := renderUsage(unknown); !strings.Contains(out, "unknown ·") {
		t.Fatalf("unattributed bucket must be labelled:\n%s", out)
	}
}

func TestSessionUsageRenderCacheWrite(t *testing.T) {
	var u core.Usage
	u.Add("M", ai.Usage{PromptTokens: 1000, CompletionTokens: 100, CacheWriteTokens: 900, CacheReported: true})
	out := renderUsage(u)
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

	m.Usage.Add("M", ai.Usage{PromptTokens: 100, CompletionTokens: 10})
	n := len(m.messages)
	m.reportUsage()
	if len(m.messages) != n+1 {
		t.Fatalf("messages = %d, want %d", len(m.messages), n+1)
	}
	if got := m.messages[len(m.messages)-1].Text; !strings.Contains(got, "Usage · 1 response") {
		t.Fatalf("report = %q", got)
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
	m.Log = sess
	m.Cfg = testFooterCfg()
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
	if m.Usage.Responses != 1 || m.Usage.Total != 1200 {
		t.Fatalf("live usage = %+v", m.Usage)
	}
	if m.ContextTokens != 1200 {
		t.Fatalf("contextTokens = %d", m.ContextTokens)
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
	if recs[0].Model != m.Cfg.ModelName() {
		t.Fatalf("persisted model = %q, want %q", recs[0].Model, m.Cfg.ModelName())
	}
	if got := core.UsageFromRecords(recs); got.Total != m.Usage.Total || got.Input != m.Usage.Input {
		t.Fatalf("resumed totals = %+v, want %+v", got, m.Usage)
	}
}

// A resumed session starts with the totals already accumulated, and /clear
// drops them.
func TestApplySessionSeedsAndClearsUsage(t *testing.T) {
	m := testModel()
	recs := []session.Record{
		{Role: session.RoleAgent, Text: "a", Model: "M1", Usage: &ai.Usage{PromptTokens: 100, CompletionTokens: 10}},
	}
	m.Usage.Add("M0", ai.Usage{PromptTokens: 9, CompletionTokens: 1})
	m.applySession(nil, recs, nil)
	if m.Usage.Responses != 1 || m.Usage.Total != 110 {
		t.Fatalf("resume totals = %+v", m.Usage)
	}
	m.applySession(nil, nil, nil)
	if !m.Usage.Empty() {
		t.Fatalf("new session must zero usage: %+v", m.Usage)
	}
}
