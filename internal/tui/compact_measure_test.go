package tui

import (
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
)

// assistantTurn feeds one completed assistant message through the live handler,
// the way the agent loop does, and returns the model.
func assistantTurn(t *testing.T, m Model, text string, usage ai.Usage) Model {
	t.Helper()
	m.turn = &turnSession{activeTool: -1}
	msg := turnAssistantMsg{message: ai.Message{Role: ai.RoleAssistant, Text: text}, usage: usage}
	if cmd := m.handleTurnAssistant(msg); cmd == nil {
		t.Fatal("expected a wait command")
	}
	return m
}

// A reported turn must record not just the token count but what it covers, so
// the auto-compact budget check can use it. The provider billed for the history
// as it stood, and the completion became the assistant message just appended,
// so the measurement spans the history we now hold.
func TestTurnRecordsMeasuredSpan(t *testing.T) {
	m := testModel()
	m.History = []ai.Message{
		{Role: ai.RoleUser, Text: "first"},
		{Role: ai.RoleAssistant, Text: "ok"},
	}

	m = assistantTurn(t, m, "done", ai.Usage{PromptTokens: 4_000, CompletionTokens: 100})

	if got, want := m.ContextMsgs, len(m.History); got != want {
		t.Fatalf("contextMsgs = %d, want %d", got, want)
	}
	cfg := m.CompactConfig(m.Cfg)
	if cfg.Measured != 4_100 || cfg.MeasuredMsgs != len(m.History) {
		t.Fatalf("compactConfig measurement = %d/%d, want 4100/%d",
			cfg.Measured, cfg.MeasuredMsgs, len(m.History))
	}
}

// A provider that reports nothing must leave the span unset, so the budget
// check falls back to the estimate instead of reading 0 as "fits fine".
func TestTurnWithoutUsageLeavesSpanUnset(t *testing.T) {
	m := testModel()
	m.History = []ai.Message{{Role: ai.RoleUser, Text: "hi"}}
	m = assistantTurn(t, m, "done", ai.Usage{})

	if m.ContextMsgs != 0 || m.ContextTokens != 0 {
		t.Fatalf("unreported usage left contextTokens=%d contextMsgs=%d", m.ContextTokens, m.ContextMsgs)
	}
	cfg := m.CompactConfig(m.Cfg)
	if cfg.Measured != 0 || cfg.MeasuredMsgs != 0 {
		t.Fatalf("unreported usage carried a measurement: %+v", cfg)
	}
}

// The end-to-end point: auto-compact must fire off the provider's count, not a
// character estimate that under-counts source code and tool output.
func TestAutoCompactUsesMeasurement(t *testing.T) {
	m := testModel()
	m.Cfg = testClientCfg() // 128k window
	m.ApplyClient()
	if m.Client == nil {
		t.Fatal("expected a client")
	}
	m.History = []ai.Message{
		// Big enough that Select has a head to free: the newest turn alone
		// stays under DefaultKeep, the older one does not.
		{Role: ai.RoleUser, Text: strings.Repeat("old ", 20_000)}, // ~20k est tokens
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "recent"},
	}

	// No measurement: the estimate (~23k) is well inside the 128k window.
	if m.ShouldAutoCompact(m.Client, m.Cfg) {
		t.Fatal("small unmeasured history should not auto-compact")
	}

	// The provider says the last request was already past the window minus the
	// buffer (128k - 20k = 108k). The estimate still says a few thousand.
	m.ContextTokens = 110_000
	m.ContextMsgs = len(m.History)
	if !m.ShouldAutoCompact(m.Client, m.Cfg) {
		t.Fatal("a measurement over budget must trigger auto-compact")
	}

	// Just under, and it must not.
	m.ContextTokens = 107_000
	if m.ShouldAutoCompact(m.Client, m.Cfg) {
		t.Fatal("a measurement under budget must not trigger auto-compact")
	}
}

// Changing model, mode, or session changes the request prefix, so a
// measurement from the old one describes a request the session will not send.
func TestResetUsageClearsMeasurement(t *testing.T) {
	m := testModel()
	m.ContextTokens = 90_000
	m.ContextMsgs = 12
	m.ResetContext()
	if m.ContextTokens != 0 || m.ContextMsgs != 0 {
		t.Fatalf("resetUsage left contextTokens=%d contextMsgs=%d", m.ContextTokens, m.ContextMsgs)
	}
	if m.CompactConfig(m.Cfg).Measured != 0 {
		t.Fatal("a cleared model must not report a measurement")
	}
}

// Compaction rewrites history, so the old measurement is meaningless. The
// re-based span must still be self-consistent: it covers the new history, so
// the next budget check counts only what is appended afterwards.
func TestCompactRebasesMeasurement(t *testing.T) {
	m := testModel()
	m.History = []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("old ", 500)},
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "recent"},
	}
	m.ContextTokens = 500_000 // a huge pre-compaction measurement
	m.ContextMsgs = len(m.History)

	res := compact.Result{
		History: []ai.Message{
			compact.CheckpointMessage("## Task\n- ship it"),
			{Role: ai.RoleUser, Text: "recent"},
		},
		Summary:   "## Task\n- ship it",
		TailCount: 1,
		Compacted: true,
	}
	m.applyCompactResult(res)

	if m.ContextMsgs != len(m.History) {
		t.Fatalf("contextMsgs = %d, want %d", m.ContextMsgs, len(m.History))
	}
	if m.ContextTokens >= 500_000 {
		t.Fatalf("stale measurement survived compaction: %d", m.ContextTokens)
	}
	cfg := m.CompactConfig(m.Cfg)
	if cfg.MeasuredMsgs != len(m.History) {
		t.Fatalf("measured span = %d, want %d", cfg.MeasuredMsgs, len(m.History))
	}
	// The stale 500k must not be carried into a budget check on the shorter
	// history, which would compact again immediately and forever.
	if m.ShouldAutoCompact(m.Client, m.Cfg) {
		t.Fatal("post-compaction state must not re-trigger compaction")
	}
}
