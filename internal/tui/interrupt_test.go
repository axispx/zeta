package tui

import (
	"path/filepath"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/image"
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

func TestTryInterruptRestoresUnstartedPrompt(t *testing.T) {
	m := testModel()
	m.messages = []Message{{Role: RoleUser, Text: "fix the flaky test"}}
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "fix the flaky test"}}
	cancelled := false
	m.turn = &turnSession{cancel: func() { cancelled = true }, ch: closedAgentEvents(), activeTool: -1}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if !cancelled || m.turn != nil {
		t.Fatal("turn not finished")
	}
	if got := m.textarea.Value(); got != "fix the flaky test" {
		t.Fatalf("composer=%q", got)
	}
	if len(m.messages) != 0 {
		t.Fatalf("user row should be uncommitted: %+v", m.messages)
	}
	if len(m.history) != 0 {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestTryInterruptRestoresUnstartedPromptWithImage(t *testing.T) {
	img := image.Ref{URL: "data:image/png;base64,AAAA", MIME: "image/png", Name: "shot.png"}
	m := testModel()
	m.messages = []Message{{Role: RoleUser, Text: userDisplayText("look", []image.Ref{img})}}
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "look", Images: []image.Ref{img}}}
	m.turn = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	text, imgs := m.parseComposer()
	if text != "look" || len(imgs) != 1 || imgs[0].Name != "shot.png" {
		t.Fatalf("composer text=%q imgs=%+v", text, imgs)
	}
	if len(m.messages) != 0 || len(m.history) != 0 {
		t.Fatalf("messages=%+v history=%+v", m.messages, m.history)
	}
}

func TestTryInterruptAfterProgressKeepsPrompt(t *testing.T) {
	m := testModel()
	m.messages = []Message{{Role: RoleUser, Text: "keep me"}}
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "keep me"}}
	m.turn = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1, progressed: true}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.textarea.Value() != "" {
		t.Fatalf("composer=%q", m.textarea.Value())
	}
	if n := len(m.messages); n != 2 || m.messages[0].Text != "keep me" || m.messages[1].Text != turnCancelledText {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestTryInterruptDraftBlocksRestore(t *testing.T) {
	m := testModel()
	m.messages = []Message{{Role: RoleUser, Text: "original"}}
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "original"}}
	m.textarea.SetValue("follow-up draft")
	m.turn = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.textarea.Value() != "follow-up draft" {
		t.Fatalf("draft overwritten: %q", m.textarea.Value())
	}
	if n := len(m.messages); n != 2 || m.messages[0].Text != "original" || m.messages[1].Text != turnCancelledText {
		t.Fatalf("messages=%+v", m.messages)
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
	m.sess = sess
	m.commitUserPrompt("ship it", nil)
	m.turn = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.textarea.Value() != "ship it" {
		t.Fatalf("composer=%q", m.textarea.Value())
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
	m.authRetrying = true
	m.messages = []Message{{Role: RoleUser, Text: "hello"}}
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "hello"}}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.authRetrying {
		t.Fatal("auth retry should clear")
	}
	if m.textarea.Value() != "hello" {
		t.Fatalf("composer=%q", m.textarea.Value())
	}
	if len(m.messages) != 0 {
		t.Fatalf("messages=%+v", m.messages)
	}
}
