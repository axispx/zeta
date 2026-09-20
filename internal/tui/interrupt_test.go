package tui

import (
	"path/filepath"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/core"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/session"
)

func TestTryInterruptCancelsTurn(t *testing.T) {
	m := testModel()
	cancelled := false
	m.turn.current = &turnSession{cancel: func() { cancelled = true }, ch: closedAgentEvents(), activeTool: -1}
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if !cancelled || m.turn.current != nil {
		t.Fatal("turn not finished")
	}
	if n := len(m.transcript.messages); n == 0 || m.transcript.messages[n-1].Text != turnCancelledText {
		t.Fatalf("expected Cancelled in transcript, messages=%+v", m.transcript.messages)
	}
	if m.transcript.messages[len(m.transcript.messages)-1].Role != RoleSystem {
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
	m := Model{session: core.Session{Compacting: true}}
	cancelled := false
	m.turn.compactCancel = func() { cancelled = true }
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if !cancelled {
		t.Fatal("compact not cancelled")
	}
	if m.turn.compactCancel != nil {
		t.Fatal("compactCancel should be nil after cancelCompact")
	}
}

func TestTryInterruptPriorityConfigOverTurn(t *testing.T) {
	m := testModel()
	turnCancelled := false
	m.config.active = true
	m.turn.current = &turnSession{cancel: func() { turnCancelled = true }, ch: closedAgentEvents(), activeTool: -1}
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.config.active {
		t.Fatal("config should be dismissed first")
	}
	if turnCancelled || m.turn.current == nil {
		t.Fatal("turn should still be active under config")
	}
	if len(m.transcript.messages) != 0 {
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
	m.turn.current = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}
	m.finishTurn()
	if len(m.transcript.messages) != 0 {
		t.Fatalf("finishTurn should not append messages: %+v", m.transcript.messages)
	}
}

func TestTryInterruptRestoresUnstartedPrompt(t *testing.T) {
	m := testModel()
	m.transcript.messages = []Message{{Role: RoleUser, Text: "fix the flaky test"}}
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "fix the flaky test"}}
	cancelled := false
	m.turn.current = &turnSession{cancel: func() { cancelled = true }, ch: closedAgentEvents(), activeTool: -1}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if !cancelled || m.turn.current != nil {
		t.Fatal("turn not finished")
	}
	if got := m.composer.textarea.Value(); got != "fix the flaky test" {
		t.Fatalf("composer=%q", got)
	}
	if len(m.transcript.messages) != 0 {
		t.Fatalf("user row should be uncommitted: %+v", m.transcript.messages)
	}
	if len(m.session.History) != 0 {
		t.Fatalf("history=%+v", m.session.History)
	}
}

func TestTryInterruptRestoresUnstartedPromptWithImage(t *testing.T) {
	img := image.Ref{URL: "data:image/png;base64,AAAA", MIME: "image/png", Name: "shot.png"}
	m := testModel()
	m.transcript.messages = []Message{{Role: RoleUser, Text: userDisplayText("look", []image.Ref{img})}}
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "look", Images: []image.Ref{img}}}
	m.turn.current = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	text, imgs := m.parseComposer()
	if text != "look" || len(imgs) != 1 || imgs[0].Name != "shot.png" {
		t.Fatalf("composer text=%q imgs=%+v", text, imgs)
	}
	if len(m.transcript.messages) != 0 || len(m.session.History) != 0 {
		t.Fatalf("messages=%+v history=%+v", m.transcript.messages, m.session.History)
	}
}

func TestTryInterruptAfterProgressKeepsPrompt(t *testing.T) {
	m := testModel()
	m.transcript.messages = []Message{{Role: RoleUser, Text: "keep me"}}
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "keep me"}}
	m.turn.current = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}
	m.session.Streamed = true

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.composer.textarea.Value() != "" {
		t.Fatalf("composer=%q", m.composer.textarea.Value())
	}
	if n := len(m.transcript.messages); n != 2 || m.transcript.messages[0].Text != "keep me" || m.transcript.messages[1].Text != turnCancelledText {
		t.Fatalf("messages=%+v", m.transcript.messages)
	}
}

func TestTryInterruptDraftBlocksRestore(t *testing.T) {
	m := testModel()
	m.transcript.messages = []Message{{Role: RoleUser, Text: "original"}}
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "original"}}
	m.composer.textarea.SetValue("follow-up draft")
	m.turn.current = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.composer.textarea.Value() != "follow-up draft" {
		t.Fatalf("draft overwritten: %q", m.composer.textarea.Value())
	}
	if n := len(m.transcript.messages); n != 2 || m.transcript.messages[0].Text != "original" || m.transcript.messages[1].Text != turnCancelledText {
		t.Fatalf("messages=%+v", m.transcript.messages)
	}
}

func TestTryInterruptRestoresUnstartedPromptFromDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")
	sess, err := session.New(proj)
	if err != nil {
		t.Fatal(err)
	}

	m := testModel()
	m.session.Log = sess
	m.commitUserPrompt("ship it", nil)
	m.turn.current = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.composer.textarea.Value() != "ship it" {
		t.Fatalf("composer=%q", m.composer.textarea.Value())
	}
	if sess.Persisted() {
		t.Fatal("empty session should be unpersisted")
	}
	entries, err := session.List(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("index=%+v", entries)
	}
}

func TestTryInterruptAuthRetryRestoresPrompt(t *testing.T) {
	m := testModel()
	m.session.AuthRetrying = true
	m.transcript.messages = []Message{{Role: RoleUser, Text: "hello"}}
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "hello"}}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.session.AuthRetrying {
		t.Fatal("auth retry should clear")
	}
	if m.composer.textarea.Value() != "hello" {
		t.Fatalf("composer=%q", m.composer.textarea.Value())
	}
	if len(m.transcript.messages) != 0 {
		t.Fatalf("messages=%+v", m.transcript.messages)
	}
}
