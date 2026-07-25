package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/session"
)

func testModel() Model {
	return Model{
		textarea: textarea.New(),
		viewport: viewport.New(),
		width:    80,
		height:   24,
		ready:    true,
		grants:   &permission.Session{},
	}
}

func TestLoadSessionRebuildsAfterCompact(t *testing.T) {
	// Explicit Tail on compact event (durable wire format).
	old := session.Record{Role: session.RoleUser, Text: strings.Repeat("old ", 500)}
	midAsst := session.Record{Role: session.RoleAgent, Text: "working"}
	recent := session.Record{Role: session.RoleUser, Text: "recent question"}
	summary := "## Task\n- ship it"
	follow := session.Record{Role: session.RoleUser, Text: "and then?"}
	// Keep only the recent user message as the retained tail.
	recs := []session.Record{
		old, midAsst, recent,
		{Role: session.RoleCompact, Text: summary, Tail: 1},
		follow,
	}

	ui, hist := loadSession(recs)

	if len(ui) < 4 {
		t.Fatalf("ui len=%d", len(ui))
	}
	foundDivider := false
	for _, m := range ui {
		if m.Role == RoleSystem && m.Text == compactDividerText {
			foundDivider = true
			break
		}
	}
	if !foundDivider {
		t.Fatalf("missing compact divider in ui: %+v", ui)
	}

	if len(hist) != 3 { // checkpoint + recent + follow
		t.Fatalf("hist len=%d: %+v", len(hist), hist)
	}
	if !compact.IsCheckpoint(hist[0]) {
		t.Fatalf("hist[0] not checkpoint: %+v", hist[0])
	}
	gotSum, ok := compact.ParseSummary(hist[0])
	if !ok || gotSum != summary {
		t.Fatalf("summary=%q ok=%v", gotSum, ok)
	}
	if hist[1].Text != recent.Text {
		t.Fatalf("tail=%+v", hist[1])
	}
	if hist[2].Text != follow.Text {
		t.Fatalf("follow-up missing: %+v", hist[len(hist)-1])
	}
}

func TestLoadSessionCompactEmptyTail(t *testing.T) {
	recs := []session.Record{
		{Role: session.RoleUser, Text: strings.Repeat("old ", 500)},
		{Role: session.RoleUser, Text: "recent"},
		{Role: session.RoleCompact, Text: "## Task\n- x", Tail: 0},
	}
	_, hist := loadSession(recs)
	if len(hist) != 1 || !compact.IsCheckpoint(hist[0]) {
		t.Fatalf("hist=%+v", hist)
	}
}

func TestLoadSessionNoCompact(t *testing.T) {
	recs := []session.Record{
		{Role: session.RoleUser, Text: "hi"},
		{Role: session.RoleAgent, Text: "hello"},
	}
	ui, hist := loadSession(recs)
	if len(ui) != 2 || len(hist) != 2 {
		t.Fatalf("ui=%d hist=%d", len(ui), len(hist))
	}
	if hist[0].Role != ai.RoleUser || hist[1].Role != ai.RoleAssistant {
		t.Fatalf("hist roles: %+v", hist)
	}
}

func TestLoadSessionDeniedTool(t *testing.T) {
	recs := []session.Record{
		{Role: session.RoleTool, Text: "rejected: the user denied this call", Label: "bash echo", Tool: "bash", Denied: true},
		{Role: session.RoleTool, Text: "ok", Label: "edit a.go", Tool: "edit", Denied: false},
	}
	ui, _ := loadSession(recs)
	if len(ui) != 2 {
		t.Fatalf("ui=%d", len(ui))
	}
	if ui[0].Status != ToolDenied || ui[0].Out != "" {
		t.Fatalf("denied: %+v", ui[0])
	}
	if ui[1].Status != ToolOK || ui[1].Out == "" {
		t.Fatalf("allowed edit should keep Out: %+v", ui[1])
	}
}

func TestHandleCompactDone(t *testing.T) {
	m := testModel()
	m.history = []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("old ", 100)},
		{Role: ai.RoleUser, Text: "new"},
	}
	m.compacting = true
	sum := "## Task\n- done"
	cp := compact.CheckpointMessage(sum)
	cmd := m.handleCompactDone(compactDoneMsg{
		result: compact.Result{
			History:   []ai.Message{cp, {Role: ai.RoleUser, Text: "new"}},
			Summary:   sum,
			TailCount: 1,
			Compacted: true,
		},
		kind: compactManual,
	})
	if cmd != nil {
		t.Fatal("manual compact should not start a turn")
	}
	if m.compacting {
		t.Fatal("compacting still set")
	}
	if len(m.history) != 2 || !compact.IsCheckpoint(m.history[0]) {
		t.Fatalf("history=%+v", m.history)
	}
	if len(m.messages) == 0 || m.messages[len(m.messages)-1].Text != compactDividerText {
		t.Fatalf("messages=%+v", m.messages)
	}
	if m.contextTokens <= 0 {
		t.Fatal("contextTokens should be estimated after compact")
	}
}

func TestHandleCompactDoneNothing(t *testing.T) {
	m := testModel()
	m.compacting = true
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "x"}}
	cmd := m.handleCompactDone(compactDoneMsg{
		result: compact.Result{History: m.history},
		kind:   compactManual,
	})
	if cmd != nil {
		t.Fatal("manual noop should not start a turn")
	}
	if m.compacting {
		t.Fatal("compacting still set")
	}
	if len(m.messages) == 0 || m.messages[0].Text != compactNothingText {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestHandleCompactDoneAutoContinuesTurn(t *testing.T) {
	m := testModel()
	m.client = &ai.Client{}
	m.compacting = true
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "hi"}}
	sum := "## Task\n- go"
	cp := compact.CheckpointMessage(sum)
	cmd := m.handleCompactDone(compactDoneMsg{
		result: compact.Result{
			History:   []ai.Message{cp, {Role: ai.RoleUser, Text: "hi"}},
			Summary:   sum,
			TailCount: 1,
			Compacted: true,
		},
		kind:        compactAuto,
		titlePrompt: "hi",
	})
	if cmd == nil {
		t.Fatal("auto compact should begin turn")
	}
	if m.turn == nil {
		t.Fatal("turn not started")
	}
	// Cancel so the background agent exits cleanly.
	m.finishTurn()
	if !compact.IsCheckpoint(m.history[0]) {
		t.Fatalf("history not applied: %+v", m.history)
	}
}

func TestHandleCompactDoneAutoFailureContinues(t *testing.T) {
	m := testModel()
	m.client = &ai.Client{}
	m.compacting = true
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "hi"}}
	cmd := m.handleCompactDone(compactDoneMsg{
		err:         errAutoCompact,
		kind:        compactAuto,
		titlePrompt: "hi",
	})
	if cmd == nil {
		t.Fatal("auto failure should still begin turn")
	}
	if m.turn == nil {
		t.Fatal("turn not started")
	}
	m.finishTurn()
	if len(m.messages) == 0 || m.messages[0].Text != compactAutoFailText {
		t.Fatalf("messages=%+v", m.messages)
	}
	if m.history[0].Text != "hi" {
		t.Fatalf("history should be unchanged: %+v", m.history)
	}
}

func TestHandleCompactDoneCancelledManual(t *testing.T) {
	m := testModel()
	m.compacting = true
	// Run wraps errors as "compact: %w"; errors.Is still matches Canceled.
	cmd := m.handleCompactDone(compactDoneMsg{
		err:  fmt.Errorf("compact: %w", context.Canceled),
		kind: compactManual,
	})
	if cmd != nil {
		t.Fatal("manual cancel should not start turn")
	}
	if len(m.messages) == 0 || m.messages[0].Text != compactCancelledText {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestShouldAutoCompact(t *testing.T) {
	m := testModel()
	m.client = &ai.Client{}
	setWindow := func(n int) {
		m.cfg = config.Config{
			Active: "p/m",
			Providers: map[string]config.Provider{
				"p": {BaseURL: "http://example", APIKey: "k", Models: map[string]config.ModelDef{
					"m": {ContextWindow: n},
				}},
			},
		}
	}
	setWindow(200_000)
	if m.shouldAutoCompact() {
		t.Fatal("empty history")
	}
	// Small history under budget.
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "hi"}}
	if m.shouldAutoCompact() {
		t.Fatal("small history should not auto-compact")
	}
	// Oversized multi-turn history on a tight window (freeable head).
	setWindow(8_000)
	m.history = []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("word ", 50_000)},
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "recent"},
	}
	if !m.shouldAutoCompact() {
		t.Fatal("large multi-turn history should auto-compact")
	}
	// Single oversized turn: over budget but nothing freeable.
	m.history = []ai.Message{{Role: ai.RoleUser, Text: strings.Repeat("word ", 50_000)}}
	if m.shouldAutoCompact() {
		t.Fatal("single oversized turn should not auto-compact")
	}
	// No window → never.
	setWindow(0)
	m.history = []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("word ", 50_000)},
		{Role: ai.RoleUser, Text: "recent"},
	}
	if m.shouldAutoCompact() {
		t.Fatal("zero window should not auto-compact")
	}
}

// errAutoCompact is a sentinel for auto-failure tests.
var errAutoCompact = errString("summarizer down")

type errString string

func (e errString) Error() string { return string(e) }

func TestStartCompactGuards(t *testing.T) {
	m := testModel()
	if cmd := m.startCompact(); cmd != nil {
		t.Fatal("nil client should not start cmd")
	}
	if len(m.messages) == 0 || m.messages[0].Text != compactNoClientText {
		t.Fatalf("messages=%+v", m.messages)
	}

	m = testModel()
	m.client = &ai.Client{}
	// Manual compact does not require a context window.
	if cmd := m.startCompact(); cmd != nil {
		t.Fatal("empty history should not start cmd")
	}
	if m.messages[0].Text != compactNothingText {
		t.Fatalf("messages=%+v", m.messages)
	}

	m = testModel()
	m.client = &ai.Client{}
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "x"}, {Role: ai.RoleUser, Text: "y"}}
	if cmd := m.startCompact(); cmd == nil {
		t.Fatal("manual compact with history should start (window optional)")
	}
	if !m.busy() || !m.compacting {
		t.Fatal("should be busy/compacting")
	}
	m.cancelCompact()
	// Drain the in-flight cmd by marking done.
	m.clearCompactState()
}

func TestBusy(t *testing.T) {
	m := testModel()
	if m.busy() {
		t.Fatal("idle")
	}
	m.compacting = true
	if !m.busy() {
		t.Fatal("compacting")
	}
	m.compacting = false
	m.turn = &turnSession{}
	if !m.busy() {
		t.Fatal("turn")
	}
}
