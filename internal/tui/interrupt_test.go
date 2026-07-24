package tui

import (
	"testing"

	"github.com/axispx/zeta/internal/session"
)

func TestTryInterruptCancelsTurn(t *testing.T) {
	m := testModel()
	cancelled := false
	m.turn = &turnSession{cancel: func() { cancelled = true }, ch: closedAgentEvents(), activeTool: -1}
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if !cancelled || m.turn != nil {
		t.Fatal("turn not finished")
	}
	if n := len(m.messages); n == 0 || m.messages[n-1].Text != turnCancelledText {
		t.Fatalf("expected Cancelled in transcript, messages=%+v", m.messages)
	}
	if m.messages[len(m.messages)-1].Role != RoleSystem {
		t.Fatal("Cancelled should be a system message")
	}
}

func TestTryInterruptDismissesPicker(t *testing.T) {
	m := Model{picker: pickerState{active: true, entries: []session.IndexEntry{{ID: "a"}}}}
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.picker.active {
		t.Fatal("picker still active")
	}
}

func TestTryInterruptDismissesConfig(t *testing.T) {
	m := Model{}
	m.config.active = true
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.config.active {
		t.Fatal("config still active")
	}
}

func TestTryInterruptDismissesModelOverlay(t *testing.T) {
	m := testModel()
	m.overlay.mode = overlayModels
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.overlay.mode != overlayOff {
		t.Fatalf("overlay mode=%v", m.overlay.mode)
	}
}

func TestTryInterruptDismissesCommandOverlay(t *testing.T) {
	m := testModel()
	m.overlay.mode = overlayCommands
	m.overlay.cmds = []command{{name: "/clear"}}
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.overlay.mode != overlayOff {
		t.Fatalf("overlay mode=%v", m.overlay.mode)
	}
}

func TestTryInterruptCancelsCompact(t *testing.T) {
	m := Model{compacting: true}
	cancelled := false
	m.compactCancel = func() { cancelled = true }
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if !cancelled {
		t.Fatal("compact not cancelled")
	}
	if m.compactCancel != nil {
		t.Fatal("compactCancel should be nil after cancelCompact")
	}
}

func TestTryInterruptPriorityConfigOverTurn(t *testing.T) {
	m := testModel()
	turnCancelled := false
	m.config.active = true
	m.turn = &turnSession{cancel: func() { turnCancelled = true }, ch: closedAgentEvents(), activeTool: -1}
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.config.active {
		t.Fatal("config should be dismissed first")
	}
	if turnCancelled || m.turn == nil {
		t.Fatal("turn should still be active under config")
	}
	if len(m.messages) != 0 {
		t.Fatal("no Cancelled until the turn itself is interrupted")
	}
}

func TestTryInterruptIdle(t *testing.T) {
	m := Model{}
	if m.tryInterrupt() {
		t.Fatal("expected no interrupt when idle")
	}
}

func TestFinishTurnDoesNotMarkCancelled(t *testing.T) {
	// Normal turn completion must not inject Cancelled.
	m := testModel()
	m.turn = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}
	m.finishTurn()
	if len(m.messages) != 0 {
		t.Fatalf("finishTurn should not append messages: %+v", m.messages)
	}
}
