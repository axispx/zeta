package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/session"
)

// midTurnModel is a model with a provider and a live turn that has done work:
// a prompt, one tool round, and a steer the loop has not taken yet.
func midTurnModel(t *testing.T) *Model {
	t.Helper()
	m := testModel()
	m.session.Cfg = testClientCfg()
	m.session.ApplyClient()
	if m.session.Client == nil {
		t.Fatal("expected a client")
	}
	m.session.History = []ai.Message{
		{Role: ai.RoleUser, Text: "refactor it"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "1", Name: "read", Arguments: `{}`}}},
		{Role: ai.RoleTool, ToolCallID: "1", Text: "contents"},
	}
	m.session.Streamed, m.session.Effects = true, true
	m.turn.nextID = 1
	m.turn.current = &turnSession{id: 1, activeTool: -1, steers: &steerBox{}, cancel: func() {}}
	m.turn.current.steers.push(newQueuedPrompt("also tidy imports", nil))
	t.Cleanup(m.turn.cancel)
	return m
}

func compactedResult() compact.Result {
	return compact.Result{
		History: []ai.Message{
			compact.CheckpointMessage("## Task\n- refactor it"),
			{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "1", Name: "read", Arguments: `{}`}}},
			{Role: ai.RoleTool, ToolCallID: "1", Text: "contents"},
		},
		Summary:   "## Task\n- refactor it",
		TailCount: 2,
		Compacted: true,
	}
}

// The loop handing a turn back must end that loop and start a compaction, with
// the steers it never took kept for the resumed turn.
func TestTurnCompactStartsCompaction(t *testing.T) {
	m := midTurnModel(t)

	cmd, ok := m.dispatchTurnMsg(turnCompactMsg{id: 1})
	if !ok || cmd == nil {
		t.Fatalf("handled=%v cmd=%v, want a compaction command", ok, cmd)
	}
	if !m.session.Compacting {
		t.Fatal("expected compaction in flight")
	}
	if m.turn.current != nil {
		t.Fatal("the handed-back loop must be torn down")
	}
	if len(m.turn.carried) != 1 || m.turn.carried[0].text != "also tidy imports" {
		t.Fatalf("carried=%v", m.turn.carried)
	}
	m.turn.abortCompact()
}

// A late hand-back from a turn the user already cancelled must do nothing.
func TestStaleTurnCompactIgnored(t *testing.T) {
	m := midTurnModel(t)
	m.turn.current = nil
	if cmd, ok := m.dispatchTurnMsg(turnCompactMsg{id: 1}); !ok || cmd != nil || m.session.Compacting {
		t.Fatalf("stale hand-back acted: cmd=%v compacting=%v", cmd, m.session.Compacting)
	}
}

// After compacting, the same turn continues: new loop, compacted history, the
// carried steer re-armed — and the turn's progress marks survive, so a later
// credential failure cannot replay work that already ran.
func TestResumeAfterMidTurnCompaction(t *testing.T) {
	m := midTurnModel(t)
	m.handleTurnCompact()
	m.turn.abortCompact()

	cmd := m.handleCompactDone(compactDoneMsg{kind: compactResume, result: compactedResult()})
	if cmd == nil {
		t.Fatal("expected the turn to resume")
	}
	if m.turn.current == nil {
		t.Fatal("no live turn after resume")
	}
	if m.session.Compacting {
		t.Fatal("still compacting")
	}
	if got := m.turn.current.steers.snapshot(); len(got) != 1 || got[0].text != "also tidy imports" {
		t.Fatalf("steers after resume = %v", got)
	}
	if len(m.turn.carried) != 0 {
		t.Fatalf("carried not cleared: %v", m.turn.carried)
	}
	if !compact.IsCheckpoint(m.session.History[0]) || len(m.session.History) != 3 {
		t.Fatalf("history not compacted: %v", textOf(m.session.History))
	}
	if !m.session.Effects || !m.session.Streamed {
		t.Fatalf("resume reset the replay gate: streamed=%v effects=%v", m.session.Streamed, m.session.Effects)
	}
	if m.session.HoldCompact {
		t.Fatal("a compaction that freed history must leave the budget check on")
	}
}

// A compaction that frees nothing, or fails, still resumes the turn, but turns
// the budget check off so the loop is not handed back every round.
func TestMidTurnCompactionThatFreesNothingHolds(t *testing.T) {
	for name, msg := range map[string]compactDoneMsg{
		"nothing to free": {kind: compactResume, result: compact.Result{}},
		"summarizer down": {kind: compactResume, err: errors.New("boom")},
	} {
		t.Run(name, func(t *testing.T) {
			m := midTurnModel(t)
			m.handleTurnCompact()
			m.turn.abortCompact()

			if cmd := m.handleCompactDone(msg); cmd == nil {
				t.Fatal("expected the turn to resume")
			}
			if m.turn.current == nil || !m.session.HoldCompact {
				t.Fatalf("live=%v hold=%v", m.turn.current != nil, m.session.HoldCompact)
			}
		})
	}
}

// Esc during the compaction cancels the turn it interrupted, and the steers
// that were waiting come back instead of being lost.
func TestCancelledMidTurnCompactionEndsTurn(t *testing.T) {
	m := midTurnModel(t)
	m.handleTurnCompact()
	m.turn.abortCompact()

	if cmd := m.handleCompactDone(compactDoneMsg{kind: compactResume, err: context.Canceled}); cmd != nil {
		t.Fatalf("a cancelled compaction must not resume: %v", cmd)
	}
	if m.turn.current != nil {
		t.Fatal("cancelled compaction left a live turn")
	}
	if got := m.composer.textarea.Value(); got != "also tidy imports" {
		t.Fatalf("steer not returned to the composer: %q", got)
	}
}

// A native compaction is recorded as the provider's checkpoint, and /resume
// rebuilds exactly the history the live session continued with.
func TestNativeCompactionRoundTripsThroughLog(t *testing.T) {
	isolateZetaHome(t)
	m := testModel()
	log, err := session.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.session.Log = log
	before := []session.Record{
		{Role: session.RoleUser, Text: "first"},
		{Role: session.RoleAgent, Text: "ok"},
		{Role: session.RoleUser, Text: "second"},
	}
	for _, r := range before {
		if err := log.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	m.session.History = compact.RebuildAPIHistory(before, "gpt-5.5")

	item := ai.Compaction{Content: "ENC", Model: "gpt-5.5"}
	m.applyCompactResult(compact.Result{
		History:   compact.NativeHistory(m.session.History, item),
		Compacted: true,
		Native:    &item,
		Usage:     ai.Usage{PromptTokens: 900, CompletionTokens: 40},
	})

	if m.session.Usage.Total != 940 {
		t.Fatalf("usage total = %d, want the compaction request counted", m.session.Usage.Total)
	}
	_, recs, err := session.OpenID(log.Cwd, log.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := recs[len(recs)-1]
	if last.Role != session.RoleCompact || last.Native != "ENC" || last.NativeModel != "gpt-5.5" || last.Usage == nil {
		t.Fatalf("record = %+v", last)
	}
	rebuilt := compact.RebuildAPIHistory(recs, "gpt-5.5")
	if got, want := textOf(rebuilt), textOf(m.session.History); strings.Join(got, ";") != strings.Join(want, ";") {
		t.Fatalf("resumed history %v differs from live %v", got, want)
	}
	if n := len(rebuilt); n != 3 || rebuilt[2].Role != ai.RoleCompaction {
		t.Fatalf("history = %v", textOf(rebuilt))
	}
}
