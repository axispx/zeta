package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/axispx/zeta/internal/image"
)

func testModelWithClient() Model {
	m := testModel()
	m.session.Cfg = testClientCfg()
	m.session.ApplyClient()
	return m
}

func qp(id int, text string) queuedPrompt {
	return queuedPrompt{id: id, text: text, display: text}
}

func TestEnterMidTurnQueues(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn.current = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.composer.textarea.SetValue("later")

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	mm := next.(Model)
	if cancelled {
		t.Fatal("composer enter must not cancel turn")
	}
	if mm.turn.current == nil {
		t.Fatal("turn should stay active")
	}
	if len(mm.queue.prompts) != 1 || mm.queue.prompts[0].text != "later" {
		t.Fatalf("queue=%+v", mm.queue.prompts)
	}
	if len(mm.session.History) != 0 {
		t.Fatalf("history=%+v", mm.session.History)
	}
	if mm.composer.textarea.Value() != "" {
		t.Fatalf("input=%q", mm.composer.textarea.Value())
	}
}

func TestEmptyEnterMidTurnSendsQueueHead(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn.current = &turnSession{
		id:         1,
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.turn.nextID = 1
	m.queue.prompts = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	if !cancelled {
		t.Fatal("send-now must cancel the live turn")
	}
	if len(m.session.History) != 1 || m.session.History[0].Text != "head" {
		t.Fatalf("history=%+v", m.session.History)
	}
	if len(m.queue.prompts) != 1 || m.queue.prompts[0].id != 2 {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
	if m.turn.current == nil || m.turn.current.id != 2 {
		t.Fatalf("new turn id=%v", m.turn.current)
	}
	// Stale Done from the aborted turn must not drain the remaining queue.
	next, doneCmd := m.Update(turnDoneMsg{id: 1})
	mm := next.(Model)
	if doneCmd != nil {
		t.Fatal("stale Done must be ignored")
	}
	if len(mm.queue.prompts) != 1 || mm.queue.prompts[0].id != 2 {
		t.Fatalf("queue after stale Done=%+v", mm.queue.prompts)
	}
	if mm.turn.current == nil || mm.turn.current.id != 2 {
		t.Fatal("live turn must survive stale Done")
	}
	mm.finishTurn()
}

func TestEmptyEnterMidTurnNoQueueNoop(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn.current = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	_ = m.submitInput()
	if cancelled {
		t.Fatal("empty enter with empty queue must not cancel")
	}
	if len(m.session.History) != 0 {
		t.Fatalf("history=%+v", m.session.History)
	}
}

func TestEmptyEnterMidTurnSkipsEditingHead(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn.current = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue.prompts = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit")
	}
	// Empty the composer without cancelEdit so editID still points at head.
	m.composer.textarea.SetValue("")
	_ = m.submitInput()
	if cancelled {
		t.Fatal("must not send head while editing it")
	}
	if m.queue.editID != 1 || len(m.queue.prompts) != 2 {
		t.Fatalf("editID=%d queue=%+v", m.queue.editID, m.queue.prompts)
	}
}

func TestQueueEnterSendsSelected(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn.current = &turnSession{
		id:         1,
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.turn.nextID = 1
	m.queue.prompts = []queuedPrompt{qp(1, "first"), qp(2, "second")}
	if !m.focusQueue() {
		t.Fatal("focus")
	}
	// newest selected; up to first
	if _, ok := m.handleQueueNavKey(tea.KeyPressMsg{Code: tea.KeyUp, Text: "up"}); !ok {
		t.Fatal("up")
	}
	if m.queue.selectedID() != 1 {
		t.Fatalf("id=%d", m.queue.selectedID())
	}
	// Full Update path — Enter must hit deliverQueued, not submitInput.
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	mm := next.(Model)
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	if !cancelled {
		t.Fatal("send-now must cancel turn")
	}
	if mm.queue.focus {
		t.Fatal("should unfocus")
	}
	if len(mm.queue.prompts) != 1 || mm.queue.prompts[0].id != 2 {
		t.Fatalf("remaining queue=%+v", mm.queue.prompts)
	}
	if len(mm.session.History) != 1 || mm.session.History[0].Text != "first" {
		t.Fatalf("history=%+v", mm.session.History)
	}
	if mm.turn.current == nil || mm.turn.current.id != 2 {
		t.Fatalf("new turn id=%v", mm.turn.current)
	}
	mm.finishTurn()
}

func TestIdleEmptyEnterDrainsQueue(t *testing.T) {
	m := testModelWithClient()
	m.queue.prompts = []queuedPrompt{qp(1, "next")}
	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected submit")
	}
	if len(m.queue.prompts) != 0 {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
	if len(m.session.History) != 1 {
		t.Fatalf("history=%+v", m.session.History)
	}
}

func TestDrainRestoresWhenSubmitRefused(t *testing.T) {
	m := testModel() // no client
	m.queue.prompts = []queuedPrompt{qp(1, "keep me")}
	cmd := m.drainNextQueuedPrompt()
	if cmd != nil {
		t.Fatal("expected nil cmd when no client")
	}
	if len(m.queue.prompts) != 1 || m.queue.prompts[0].text != "keep me" || m.queue.prompts[0].id != 1 {
		t.Fatalf("prompt must be restored with same id: queue=%+v", m.queue.prompts)
	}
}

func TestTurnDoneDrainsQueueFIFO(t *testing.T) {
	m := testModelWithClient()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue.prompts = []queuedPrompt{qp(1, "first"), qp(2, "second")}
	cmd := m.handleTurnDone()
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	if len(m.queue.prompts) != 1 || m.queue.prompts[0].text != "second" {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
	if len(m.session.History) != 1 || m.session.History[0].Text != "first" {
		t.Fatalf("history=%+v", m.session.History)
	}
}

func TestLateDoneAfterCancelDoesNotDrainQueue(t *testing.T) {
	m := testModelWithClient()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue.prompts = []queuedPrompt{qp(1, "keep"), qp(2, "also")}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.turn.current != nil {
		t.Fatal("turn should be cleared")
	}
	cmd := m.handleTurnDone()
	if cmd != nil {
		t.Fatal("late Done after cancel must not drain queue")
	}
	if len(m.queue.prompts) != 2 {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
}

func TestEscCancelsTurnKeepsQueue(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn.current = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue.prompts = []queuedPrompt{qp(1, "keep"), qp(2, "also")}

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	mm := next.(Model)
	if !cancelled || mm.turn.current != nil {
		t.Fatal("esc should cancel turn")
	}
	if len(mm.queue.prompts) != 2 {
		t.Fatalf("queue must stay: %+v", mm.queue.prompts)
	}
}

func TestCtrlCCancelsEditFirst(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn.current = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue.prompts = []queuedPrompt{qp(1, "a"), qp(2, "b")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit")
	}
	m.composer.textarea.SetValue("editing")

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	mm := next.(Model)
	if cmd != nil {
		t.Fatal("ctrl+c must not quit while cancelling edit")
	}
	if mm.queue.editID != 0 {
		t.Fatalf("editID=%d", mm.queue.editID)
	}
	if len(mm.queue.prompts) != 2 {
		t.Fatalf("queue should stay until a later ctrl+c: %+v", mm.queue.prompts)
	}
	if cancelled || mm.turn.current == nil {
		t.Fatal("edit cancel must not cancel turn")
	}
}

func TestCtrlCCancelsTurnBeforeClearingQueue(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn.current = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue.prompts = []queuedPrompt{qp(1, "a"), qp(2, "b")}

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	mm := next.(Model)
	if cmd != nil {
		t.Fatal("ctrl+c must not quit while cancelling turn")
	}
	if !cancelled || mm.turn.current != nil {
		t.Fatal("ctrl+c should cancel turn")
	}
	if len(mm.queue.prompts) != 2 {
		t.Fatalf("queue kept until next ctrl+c: %+v", mm.queue.prompts)
	}
	if n := len(mm.transcript.messages); n == 0 || mm.transcript.messages[n-1].Text != turnCancelledText {
		t.Fatalf("expected Cancelled, messages=%+v", mm.transcript.messages)
	}

	// Second press clears the queue.
	next, cmd = mm.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	mm = next.(Model)
	if cmd != nil {
		t.Fatal("ctrl+c clearing queue must not quit")
	}
	if mm.queue.hasState() {
		t.Fatalf("queue should clear: %+v", mm.queue.prompts)
	}
}

func TestCtrlCClearsQueueWhenIdle(t *testing.T) {
	m := testModelWithClient()
	m.queue.prompts = []queuedPrompt{qp(1, "a"), qp(2, "b")}

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	mm := next.(Model)
	if cmd != nil {
		t.Fatal("ctrl+c with queue must not quit")
	}
	if mm.queue.hasState() {
		t.Fatalf("queue should clear: %+v", mm.queue.prompts)
	}
}

func TestCtrlCQuitsWhenIdleNoQueue(t *testing.T) {
	m := testModelWithClient()
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	_ = next
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
}

func TestDeliverQueuedRestoresOriginalIndex(t *testing.T) {
	m := testModel() // no client → submit refuses
	m.queue.prompts = []queuedPrompt{qp(1, "a"), qp(2, "b"), qp(3, "c")}
	cmd := m.deliverQueued(2)
	if cmd != nil {
		t.Fatal("expected nil cmd when no client")
	}
	if len(m.queue.prompts) != 3 {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
	if m.queue.prompts[0].id != 1 || m.queue.prompts[1].id != 2 || m.queue.prompts[2].id != 3 {
		t.Fatalf("order restored wrong: %+v", m.queue.prompts)
	}
}

func TestEditHeadBlocksDrain(t *testing.T) {
	m := testModelWithClient()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue.prompts = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit head")
	}
	cmd := m.handleTurnDone()
	if cmd != nil {
		t.Fatal("must not drain while editing head")
	}
	if len(m.queue.prompts) != 2 || m.queue.editID != 1 {
		t.Fatalf("editID=%d queue=%+v", m.queue.editID, m.queue.prompts)
	}
}

func TestEditNonHeadAllowsDrain(t *testing.T) {
	m := testModelWithClient()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue.prompts = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
	if !m.beginEdit(2) {
		t.Fatal("beginEdit tail")
	}
	cmd := m.handleTurnDone()
	if cmd == nil {
		t.Fatal("head should drain while editing tail")
	}
	if len(m.queue.prompts) != 1 || m.queue.prompts[0].id != 2 || m.queue.editID != 2 {
		t.Fatalf("editID=%d queue=%+v", m.queue.editID, m.queue.prompts)
	}
	if m.composer.textarea.Value() != "tail" {
		t.Fatalf("composer=%q", m.composer.textarea.Value())
	}
}

func TestSaveEditWritesBack(t *testing.T) {
	m := testModelWithClient()
	m.queue.prompts = []queuedPrompt{qp(1, "old"), qp(2, "keep")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit")
	}
	m.composer.textarea.SetValue("new text")
	_ = m.submitInput()
	if m.queue.editID != 0 {
		t.Fatalf("editID=%d", m.queue.editID)
	}
	if m.queue.prompts[0].text != "new text" || m.queue.prompts[0].id != 1 {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
}

func TestCancelEditOnEsc(t *testing.T) {
	m := testModelWithClient()
	m.queue.prompts = []queuedPrompt{qp(1, "keep me")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit")
	}
	m.composer.textarea.SetValue("changed")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	mm := next.(Model)
	if mm.queue.editID != 0 {
		t.Fatalf("editID=%d", mm.queue.editID)
	}
	if mm.queue.prompts[0].text != "keep me" {
		t.Fatalf("queue=%+v", mm.queue.prompts)
	}
}

func TestRemoveQueuedClearsEdit(t *testing.T) {
	m := testModel()
	m.queue.prompts = []queuedPrompt{qp(1, "a"), qp(2, "b")}
	if !m.beginEdit(2) {
		t.Fatal("beginEdit")
	}
	if !m.removeQueued(2) {
		t.Fatal("remove")
	}
	if m.queue.editID != 0 || len(m.queue.prompts) != 1 {
		t.Fatalf("editID=%d queue=%+v", m.queue.editID, m.queue.prompts)
	}
}

func TestDraftBlocksDrain(t *testing.T) {
	m := testModelWithClient()
	m.queue.prompts = []queuedPrompt{qp(1, "next")}
	m.composer.textarea.SetValue("draft")
	if m.drainNextQueuedPrompt() != nil {
		t.Fatal("draft must block drain")
	}
}

func TestEnqueueWithImages(t *testing.T) {
	isolateZetaHome(t)
	m, err := New(testClientCfg(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = m.enqueuePrompt("look", []image.Ref{{URL: testPNGDataURL, Name: "a.png"}})
	if len(m.queue.prompts) != 1 || len(m.queue.prompts[0].imgs) != 1 {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
}

func TestSlashHarnessBlockedDuringTurn(t *testing.T) {
	m := testModelWithClient()
	m.turn.current = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}
	m.composer.textarea.SetValue("/clear")
	if m.submitInput() != nil {
		t.Fatal("harness should not run mid-turn")
	}
	if len(m.queue.prompts) != 0 {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
}

func TestSlashSkillQueuesDuringTurn(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn.current = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.composer.textarea.SetValue("/review args")
	_ = m.submitInput()
	if cancelled {
		t.Fatal("must not cancel")
	}
	if len(m.queue.prompts) != 1 || !strings.HasPrefix(m.queue.prompts[0].text, "/review") {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
}

func TestClearQueueOnApplySession(t *testing.T) {
	m := testModel()
	m.queue.prompts = []queuedPrompt{qp(1, "x"), qp(2, "y")}
	m.queue.editID = 1
	m.applySession(nil, nil, nil)
	if m.queue.hasState() {
		t.Fatal("queue should clear")
	}
}

func TestModeSwitchBlockedWithQueue(t *testing.T) {
	m := testModel()
	m.queue.prompts = []queuedPrompt{qp(1, "x")}
	before := m.session.Mode
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift, Text: "shift+tab"})
	if next.(Model).session.Mode != before {
		t.Fatal("mode should not change with queue")
	}
}

func TestQueueFocusNavEditRemove(t *testing.T) {
	m := testModelWithClient()
	m.queue.prompts = []queuedPrompt{qp(1, "first"), qp(2, "second"), qp(3, "third")}

	if !m.focusQueue() {
		t.Fatal("focusQueue")
	}
	if m.queue.sel.selected != 2 {
		t.Fatalf("sel=%d want newest", m.queue.sel.selected)
	}
	if _, ok := m.handleQueueNavKey(tea.KeyPressMsg{Code: tea.KeyUp, Text: "up"}); !ok {
		t.Fatal("up")
	}
	if m.queue.sel.selected != 1 {
		t.Fatalf("sel=%d", m.queue.sel.selected)
	}
	if _, ok := m.handleQueueNavKey(tea.KeyPressMsg{Code: 'e', Text: "e"}); !ok {
		t.Fatal("e")
	}
	if m.queue.focus || m.queue.editID != 2 || m.composer.textarea.Value() != "second" {
		t.Fatalf("focus=%v editID=%d input=%q", m.queue.focus, m.queue.editID, m.composer.textarea.Value())
	}
	_ = m.cancelEdit()
	if !m.focusQueue() {
		t.Fatal("refocus")
	}
	if _, ok := m.handleQueueNavKey(tea.KeyPressMsg{Code: 'd', Text: "d"}); !ok {
		t.Fatal("d")
	}
	if len(m.queue.prompts) != 2 || m.queue.prompts[1].id != 2 {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
}

func TestQueueFocusArrowsSkipPromptHistory(t *testing.T) {
	m := testModelWithClient()
	m.transcript.messages = []Message{{Role: RoleUser, Text: "old turn"}}
	m.queue.prompts = []queuedPrompt{qp(1, "a"), qp(2, "b")}
	if !m.focusQueue() {
		t.Fatal("focus")
	}
	if m.queue.sel.selected != 1 {
		t.Fatalf("sel=%d", m.queue.sel.selected)
	}
	// Full Update: ↑ must move queue selection, not load prompt history.
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Text: "up"})
	mm := next.(Model)
	if !mm.queue.focus || mm.queue.sel.selected != 0 {
		t.Fatalf("focus=%v sel=%d", mm.queue.focus, mm.queue.sel.selected)
	}
	if mm.composer.textarea.Value() != "" {
		t.Fatalf("history must not fill composer: %q", mm.composer.textarea.Value())
	}
	if !mm.composer.promptHist.live() {
		t.Fatalf("prompt history must stay live, at=%d", mm.composer.promptHist.at)
	}
	// ↓ as well
	next, _ = mm.Update(tea.KeyPressMsg{Code: tea.KeyDown, Text: "down"})
	mm = next.(Model)
	if !mm.queue.focus || mm.queue.sel.selected != 1 {
		t.Fatalf("down: focus=%v sel=%d", mm.queue.focus, mm.queue.sel.selected)
	}
	if mm.composer.textarea.Value() != "" {
		t.Fatalf("down filled composer: %q", mm.composer.textarea.Value())
	}
}

func TestQueueFocusEscUnfocuses(t *testing.T) {
	m := testModelWithClient()
	m.turn.current = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}
	m.queue.prompts = []queuedPrompt{qp(1, "a")}
	if !m.focusQueue() {
		t.Fatal("focus")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	mm := next.(Model)
	if mm.queue.focus {
		t.Fatal("esc should unfocus")
	}
	if mm.turn.current == nil {
		t.Fatal("esc must not cancel turn while leaving focus")
	}
}

func TestCtrlQTogglesQueueFocus(t *testing.T) {
	m := testModelWithClient()
	m.queue.prompts = []queuedPrompt{qp(1, "a"), qp(2, "b")}
	next, _ := m.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl, Text: "ctrl+q"})
	mm := next.(Model)
	if !mm.queue.focus || mm.queue.sel.selected != 1 {
		t.Fatalf("focus=%v sel=%d", mm.queue.focus, mm.queue.sel.selected)
	}
	next, _ = mm.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl, Text: "ctrl+q"})
	if next.(Model).queue.focus {
		t.Fatal("second ctrl+q should unfocus")
	}
}

func TestRenderQueueFollowupsLayout(t *testing.T) {
	m := testModel()
	m.queue.prompts = []queuedPrompt{qp(1, "a")}
	plain := ansi.Strip(m.renderQueueFollowups(80))
	for _, want := range []string{
		"follow-ups",
		"○ a",
		"enter send now",
		"ctrl+q manage",
		"esc cancel turn",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in:\n%s", want, plain)
		}
	}
}

func TestRenderQueueFocusHints(t *testing.T) {
	m := testModel()
	m.queue.prompts = []queuedPrompt{qp(1, "a"), qp(2, "b")}
	if !m.focusQueue() {
		t.Fatal("focus")
	}
	plain := ansi.Strip(m.renderQueueFollowups(80))
	for _, want := range []string{
		"follow-ups · ↑/↓",
		"→ b",
		"enter send",
		"e edit",
		"d remove",
		"esc back",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in:\n%s", want, plain)
		}
	}
}

func TestRenderFollowupsMarksEditing(t *testing.T) {
	m := testModel()
	m.queue.prompts = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
	if !m.beginEdit(2) {
		t.Fatal("beginEdit non-head")
	}
	plain := ansi.Strip(m.renderQueueFollowups(80))
	for _, want := range []string{
		"follow-ups · editing",
		"✎ tail",
		"enter save",
		"esc cancel edit",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "enter send now") {
		t.Fatalf("non-head edit must not show default send hints:\n%s", plain)
	}
}
