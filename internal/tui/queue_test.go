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
	m.cfg = testClientCfg()
	m.applyClient()
	return m
}

func qp(id int, text string) queuedPrompt {
	return queuedPrompt{id: id, text: text, display: text}
}

func TestEnterMidTurnQueues(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.textarea.SetValue("later")

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	mm := next.(Model)
	if cancelled {
		t.Fatal("composer enter must not cancel turn")
	}
	if mm.turn == nil {
		t.Fatal("turn should stay active")
	}
	if len(mm.queue) != 1 || mm.queue[0].text != "later" {
		t.Fatalf("queue=%+v", mm.queue)
	}
	if len(mm.history) != 0 {
		t.Fatalf("history=%+v", mm.history)
	}
	if mm.textarea.Value() != "" {
		t.Fatalf("input=%q", mm.textarea.Value())
	}
}

func TestEmptyEnterMidTurnSendsQueueHead(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		id:         1,
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.nextTurnID = 1
	m.queue = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	if !cancelled {
		t.Fatal("send-now must cancel the live turn")
	}
	if len(m.history) != 1 || m.history[0].Text != "head" {
		t.Fatalf("history=%+v", m.history)
	}
	if len(m.queue) != 1 || m.queue[0].id != 2 {
		t.Fatalf("queue=%+v", m.queue)
	}
	if m.turn == nil || m.turn.id != 2 {
		t.Fatalf("new turn id=%v", m.turn)
	}
	// Stale Done from the aborted turn must not drain the remaining queue.
	next, doneCmd := m.Update(turnDoneMsg{id: 1})
	mm := next.(Model)
	if doneCmd != nil {
		t.Fatal("stale Done must be ignored")
	}
	if len(mm.queue) != 1 || mm.queue[0].id != 2 {
		t.Fatalf("queue after stale Done=%+v", mm.queue)
	}
	if mm.turn == nil || mm.turn.id != 2 {
		t.Fatal("live turn must survive stale Done")
	}
	mm.finishTurn()
}

func TestEmptyEnterMidTurnNoQueueNoop(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	_ = m.submitInput()
	if cancelled {
		t.Fatal("empty enter with empty queue must not cancel")
	}
	if len(m.history) != 0 {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestEmptyEnterMidTurnSkipsEditingHead(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit")
	}
	// Empty the composer without cancelEdit so editID still points at head.
	m.textarea.SetValue("")
	_ = m.submitInput()
	if cancelled {
		t.Fatal("must not send head while editing it")
	}
	if m.editID != 1 || len(m.queue) != 2 {
		t.Fatalf("editID=%d queue=%+v", m.editID, m.queue)
	}
}

func TestQueueEnterSendsSelected(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		id:         1,
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.nextTurnID = 1
	m.queue = []queuedPrompt{qp(1, "first"), qp(2, "second")}
	if !m.focusQueue() {
		t.Fatal("focus")
	}
	// newest selected; up to first
	if _, ok := m.handleQueueNavKey(tea.KeyPressMsg{Code: tea.KeyUp, Text: "up"}); !ok {
		t.Fatal("up")
	}
	if m.selectedQueueID() != 1 {
		t.Fatalf("id=%d", m.selectedQueueID())
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
	if mm.queueFocus {
		t.Fatal("should unfocus")
	}
	if len(mm.queue) != 1 || mm.queue[0].id != 2 {
		t.Fatalf("remaining queue=%+v", mm.queue)
	}
	if len(mm.history) != 1 || mm.history[0].Text != "first" {
		t.Fatalf("history=%+v", mm.history)
	}
	if mm.turn == nil || mm.turn.id != 2 {
		t.Fatalf("new turn id=%v", mm.turn)
	}
	mm.finishTurn()
}

func TestIdleEmptyEnterDrainsQueue(t *testing.T) {
	m := testModelWithClient()
	m.queue = []queuedPrompt{qp(1, "next")}
	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected submit")
	}
	if len(m.queue) != 0 {
		t.Fatalf("queue=%+v", m.queue)
	}
	if len(m.history) != 1 {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestDrainRestoresWhenSubmitRefused(t *testing.T) {
	m := testModel() // no client
	m.queue = []queuedPrompt{qp(1, "keep me")}
	cmd := m.drainNextQueuedPrompt()
	if cmd != nil {
		t.Fatal("expected nil cmd when no client")
	}
	if len(m.queue) != 1 || m.queue[0].text != "keep me" || m.queue[0].id != 1 {
		t.Fatalf("prompt must be restored with same id: queue=%+v", m.queue)
	}
}

func TestTurnDoneDrainsQueueFIFO(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{qp(1, "first"), qp(2, "second")}
	cmd := m.handleTurnDone()
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	if len(m.queue) != 1 || m.queue[0].text != "second" {
		t.Fatalf("queue=%+v", m.queue)
	}
	if len(m.history) != 1 || m.history[0].Text != "first" {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestLateDoneAfterCancelDoesNotDrainQueue(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{qp(1, "keep"), qp(2, "also")}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.turn != nil {
		t.Fatal("turn should be cleared")
	}
	cmd := m.handleTurnDone()
	if cmd != nil {
		t.Fatal("late Done after cancel must not drain queue")
	}
	if len(m.queue) != 2 {
		t.Fatalf("queue=%+v", m.queue)
	}
}

func TestEscCancelsTurnKeepsQueue(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{qp(1, "keep"), qp(2, "also")}

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	mm := next.(Model)
	if !cancelled || mm.turn != nil {
		t.Fatal("esc should cancel turn")
	}
	if len(mm.queue) != 2 {
		t.Fatalf("queue must stay: %+v", mm.queue)
	}
}

func TestCtrlCCancelsEditFirst(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{qp(1, "a"), qp(2, "b")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit")
	}
	m.textarea.SetValue("editing")

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	mm := next.(Model)
	if cmd != nil {
		t.Fatal("ctrl+c must not quit while cancelling edit")
	}
	if mm.editID != 0 {
		t.Fatalf("editID=%d", mm.editID)
	}
	if len(mm.queue) != 2 {
		t.Fatalf("queue should stay until a later ctrl+c: %+v", mm.queue)
	}
	if cancelled || mm.turn == nil {
		t.Fatal("edit cancel must not cancel turn")
	}
}

func TestCtrlCCancelsTurnBeforeClearingQueue(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{qp(1, "a"), qp(2, "b")}

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	mm := next.(Model)
	if cmd != nil {
		t.Fatal("ctrl+c must not quit while cancelling turn")
	}
	if !cancelled || mm.turn != nil {
		t.Fatal("ctrl+c should cancel turn")
	}
	if len(mm.queue) != 2 {
		t.Fatalf("queue kept until next ctrl+c: %+v", mm.queue)
	}
	if n := len(mm.messages); n == 0 || mm.messages[n-1].Text != turnCancelledText {
		t.Fatalf("expected Cancelled, messages=%+v", mm.messages)
	}

	// Second press clears the queue.
	next, cmd = mm.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	mm = next.(Model)
	if cmd != nil {
		t.Fatal("ctrl+c clearing queue must not quit")
	}
	if mm.hasQueueState() {
		t.Fatalf("queue should clear: %+v", mm.queue)
	}
}

func TestCtrlCClearsQueueWhenIdle(t *testing.T) {
	m := testModelWithClient()
	m.queue = []queuedPrompt{qp(1, "a"), qp(2, "b")}

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	mm := next.(Model)
	if cmd != nil {
		t.Fatal("ctrl+c with queue must not quit")
	}
	if mm.hasQueueState() {
		t.Fatalf("queue should clear: %+v", mm.queue)
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
	m.queue = []queuedPrompt{qp(1, "a"), qp(2, "b"), qp(3, "c")}
	cmd := m.deliverQueued(2)
	if cmd != nil {
		t.Fatal("expected nil cmd when no client")
	}
	if len(m.queue) != 3 {
		t.Fatalf("queue=%+v", m.queue)
	}
	if m.queue[0].id != 1 || m.queue[1].id != 2 || m.queue[2].id != 3 {
		t.Fatalf("order restored wrong: %+v", m.queue)
	}
}

func TestEditHeadBlocksDrain(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit head")
	}
	cmd := m.handleTurnDone()
	if cmd != nil {
		t.Fatal("must not drain while editing head")
	}
	if len(m.queue) != 2 || m.editID != 1 {
		t.Fatalf("editID=%d queue=%+v", m.editID, m.queue)
	}
}

func TestEditNonHeadAllowsDrain(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
	if !m.beginEdit(2) {
		t.Fatal("beginEdit tail")
	}
	cmd := m.handleTurnDone()
	if cmd == nil {
		t.Fatal("head should drain while editing tail")
	}
	if len(m.queue) != 1 || m.queue[0].id != 2 || m.editID != 2 {
		t.Fatalf("editID=%d queue=%+v", m.editID, m.queue)
	}
	if m.textarea.Value() != "tail" {
		t.Fatalf("composer=%q", m.textarea.Value())
	}
}

func TestSaveEditWritesBack(t *testing.T) {
	m := testModelWithClient()
	m.queue = []queuedPrompt{qp(1, "old"), qp(2, "keep")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit")
	}
	m.textarea.SetValue("new text")
	_ = m.submitInput()
	if m.editID != 0 {
		t.Fatalf("editID=%d", m.editID)
	}
	if m.queue[0].text != "new text" || m.queue[0].id != 1 {
		t.Fatalf("queue=%+v", m.queue)
	}
}

func TestCancelEditOnEsc(t *testing.T) {
	m := testModelWithClient()
	m.queue = []queuedPrompt{qp(1, "keep me")}
	if !m.beginEdit(1) {
		t.Fatal("beginEdit")
	}
	m.textarea.SetValue("changed")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	mm := next.(Model)
	if mm.editID != 0 {
		t.Fatalf("editID=%d", mm.editID)
	}
	if mm.queue[0].text != "keep me" {
		t.Fatalf("queue=%+v", mm.queue)
	}
}

func TestRemoveQueuedClearsEdit(t *testing.T) {
	m := testModel()
	m.queue = []queuedPrompt{qp(1, "a"), qp(2, "b")}
	if !m.beginEdit(2) {
		t.Fatal("beginEdit")
	}
	if !m.removeQueued(2) {
		t.Fatal("remove")
	}
	if m.editID != 0 || len(m.queue) != 1 {
		t.Fatalf("editID=%d queue=%+v", m.editID, m.queue)
	}
}

func TestDraftBlocksDrain(t *testing.T) {
	m := testModelWithClient()
	m.queue = []queuedPrompt{qp(1, "next")}
	m.textarea.SetValue("draft")
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
	if len(m.queue) != 1 || len(m.queue[0].imgs) != 1 {
		t.Fatalf("queue=%+v", m.queue)
	}
}

func TestSlashHarnessBlockedDuringTurn(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}
	m.textarea.SetValue("/clear")
	if m.submitInput() != nil {
		t.Fatal("harness should not run mid-turn")
	}
	if len(m.queue) != 0 {
		t.Fatalf("queue=%+v", m.queue)
	}
}

func TestSlashSkillQueuesDuringTurn(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.textarea.SetValue("/review args")
	_ = m.submitInput()
	if cancelled {
		t.Fatal("must not cancel")
	}
	if len(m.queue) != 1 || !strings.HasPrefix(m.queue[0].text, "/review") {
		t.Fatalf("queue=%+v", m.queue)
	}
}

func TestClearQueueOnApplySession(t *testing.T) {
	m := testModel()
	m.queue = []queuedPrompt{qp(1, "x"), qp(2, "y")}
	m.editID = 1
	m.applySession(nil, nil, nil)
	if m.hasQueueState() {
		t.Fatal("queue should clear")
	}
}

func TestModeSwitchBlockedWithQueue(t *testing.T) {
	m := testModel()
	m.queue = []queuedPrompt{qp(1, "x")}
	before := m.mode
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift, Text: "shift+tab"})
	if next.(Model).mode != before {
		t.Fatal("mode should not change with queue")
	}
}

func TestQueueFocusNavEditRemove(t *testing.T) {
	m := testModelWithClient()
	m.queue = []queuedPrompt{qp(1, "first"), qp(2, "second"), qp(3, "third")}

	if !m.focusQueue() {
		t.Fatal("focusQueue")
	}
	if m.queueSel.selected != 2 {
		t.Fatalf("sel=%d want newest", m.queueSel.selected)
	}
	if _, ok := m.handleQueueNavKey(tea.KeyPressMsg{Code: tea.KeyUp, Text: "up"}); !ok {
		t.Fatal("up")
	}
	if m.queueSel.selected != 1 {
		t.Fatalf("sel=%d", m.queueSel.selected)
	}
	if _, ok := m.handleQueueNavKey(tea.KeyPressMsg{Code: 'e', Text: "e"}); !ok {
		t.Fatal("e")
	}
	if m.queueFocus || m.editID != 2 || m.textarea.Value() != "second" {
		t.Fatalf("focus=%v editID=%d input=%q", m.queueFocus, m.editID, m.textarea.Value())
	}
	_ = m.cancelEdit()
	if !m.focusQueue() {
		t.Fatal("refocus")
	}
	if _, ok := m.handleQueueNavKey(tea.KeyPressMsg{Code: 'd', Text: "d"}); !ok {
		t.Fatal("d")
	}
	if len(m.queue) != 2 || m.queue[1].id != 2 {
		t.Fatalf("queue=%+v", m.queue)
	}
}

func TestQueueFocusArrowsSkipPromptHistory(t *testing.T) {
	m := testModelWithClient()
	m.messages = []Message{{Role: RoleUser, Text: "old turn"}}
	m.queue = []queuedPrompt{qp(1, "a"), qp(2, "b")}
	if !m.focusQueue() {
		t.Fatal("focus")
	}
	if m.queueSel.selected != 1 {
		t.Fatalf("sel=%d", m.queueSel.selected)
	}
	// Full Update: ↑ must move queue selection, not load prompt history.
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Text: "up"})
	mm := next.(Model)
	if !mm.queueFocus || mm.queueSel.selected != 0 {
		t.Fatalf("focus=%v sel=%d", mm.queueFocus, mm.queueSel.selected)
	}
	if mm.textarea.Value() != "" {
		t.Fatalf("history must not fill composer: %q", mm.textarea.Value())
	}
	if !mm.promptHist.live() {
		t.Fatalf("prompt history must stay live, at=%d", mm.promptHist.at)
	}
	// ↓ as well
	next, _ = mm.Update(tea.KeyPressMsg{Code: tea.KeyDown, Text: "down"})
	mm = next.(Model)
	if !mm.queueFocus || mm.queueSel.selected != 1 {
		t.Fatalf("down: focus=%v sel=%d", mm.queueFocus, mm.queueSel.selected)
	}
	if mm.textarea.Value() != "" {
		t.Fatalf("down filled composer: %q", mm.textarea.Value())
	}
}

func TestQueueFocusEscUnfocuses(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}
	m.queue = []queuedPrompt{qp(1, "a")}
	if !m.focusQueue() {
		t.Fatal("focus")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	mm := next.(Model)
	if mm.queueFocus {
		t.Fatal("esc should unfocus")
	}
	if mm.turn == nil {
		t.Fatal("esc must not cancel turn while leaving focus")
	}
}

func TestCtrlQTogglesQueueFocus(t *testing.T) {
	m := testModelWithClient()
	m.queue = []queuedPrompt{qp(1, "a"), qp(2, "b")}
	next, _ := m.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl, Text: "ctrl+q"})
	mm := next.(Model)
	if !mm.queueFocus || mm.queueSel.selected != 1 {
		t.Fatalf("focus=%v sel=%d", mm.queueFocus, mm.queueSel.selected)
	}
	next, _ = mm.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl, Text: "ctrl+q"})
	if next.(Model).queueFocus {
		t.Fatal("second ctrl+q should unfocus")
	}
}

func TestRenderQueueFollowupsLayout(t *testing.T) {
	m := testModel()
	m.queue = []queuedPrompt{qp(1, "a")}
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
	m.queue = []queuedPrompt{qp(1, "a"), qp(2, "b")}
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
	m.queue = []queuedPrompt{qp(1, "head"), qp(2, "tail")}
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


