package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/image"
)

func testModelWithClient() *Model {
	m := testModel()
	m.session.Cfg = testClientCfg()
	m.session.ApplyClient()
	return m
}

func qp(text string) queuedPrompt {
	return queuedPrompt{text: text, display: text}
}

func busyTurn(m *Model, cancelled *bool) {
	m.turn.current = &turnSession{
		id:         1,
		cancel:     func() { *cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
}

func TestCtrlCClearsQueueWhenIdle(t *testing.T) {
	m := testModelWithClient()
	m.queue.prompts = []queuedPrompt{qp("a"), qp("b")}

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "ctrl+c"})
	mm := next.(*Model)
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

func TestClearQueueOnApplySession(t *testing.T) {
	m := testModel()
	m.queue.prompts = []queuedPrompt{qp("x"), qp("y")}
	m.applySession(nil, nil, nil)
	if m.queue.hasState() {
		t.Fatal("queue should clear")
	}
}

func enter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"} }

func pendingSteers(m *Model) []string {
	var out []string
	for _, p := range m.turn.current.steers.snapshot() {
		out = append(out, p.text)
	}
	return out
}

func TestEnterMidTurnSteers(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	m.composer.textarea.SetValue("also this")

	m.Update(enter())
	if cancelled || m.turn.current == nil {
		t.Fatal("steer must not cancel the turn")
	}
	if got := pendingSteers(m); len(got) != 1 || got[0] != "also this" {
		t.Fatalf("steers=%v", got)
	}
	if len(m.queue.prompts) != 0 || len(m.session.History) != 0 || m.composer.textarea.Value() != "" {
		t.Fatalf("queue=%+v history=%+v input=%q", m.queue.prompts, m.session.History, m.composer.textarea.Value())
	}
}

func TestTabMidTurnQueues(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	m.composer.textarea.SetValue("later")

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Text: "tab"})
	if cancelled || len(m.queue.prompts) != 1 || m.queue.prompts[0].text != "later" {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
	if len(pendingSteers(m)) != 0 {
		t.Fatal("tab must not steer")
	}
}

func TestSkillSlashMidTurnQueues(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	m.composer.textarea.SetValue("/review args")
	_ = m.submitInput()
	if cancelled || len(pendingSteers(m)) != 0 || len(m.queue.prompts) != 1 || !strings.HasPrefix(m.queue.prompts[0].text, "/review") {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
}

func TestSteerBoxTakeDrains(t *testing.T) {
	b := &steerBox{}
	b.push(qp("a"))
	b.push(qp("b"))
	got := b.take()
	if len(got) != 2 || got[0].Text != "a" || got[1].Text != "b" || got[0].Role != ai.RoleUser {
		t.Fatalf("take=%+v", got)
	}
	if len(b.take()) != 0 {
		t.Fatal("second take must be empty")
	}
}

func TestSteerRecordedWhenLoopTakesIt(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	cmd := m.handleTurnSteer(turnSteerMsg{id: 1, message: ai.Message{Role: ai.RoleUser, Text: "nudge"}})
	if cmd == nil {
		t.Fatal("expected to keep waiting on the turn")
	}
	h := m.session.History
	if len(h) != 1 || h[0].Text != "nudge" {
		t.Fatalf("history=%+v", h)
	}
}

func TestEscWithSteersSendsOnlyTheOldest(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	m.turn.current.steers.push(qp("one"))
	m.turn.current.steers.push(qp("two"))
	m.queue.prompts = []queuedPrompt{qp("later")}

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	mm := next.(*Model)
	if !cancelled || cmd == nil {
		t.Fatal("Esc with steers must interrupt and start the next turn")
	}
	h := mm.session.History
	if len(h) != 1 || h[0].Text != "one" {
		t.Fatalf("history=%+v", h)
	}
	if got := pendingSteers(mm); len(got) != 1 || got[0] != "two" {
		t.Fatalf("remaining steers=%v", got)
	}
	if len(mm.queue.prompts) != 1 || mm.queue.prompts[0].text != "later" {
		t.Fatalf("queue=%+v", mm.queue.prompts)
	}
}

func TestEscWithOnlyQueueCancelsAndRestoresComposer(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	m.queue.prompts = []queuedPrompt{qp("one"), qp("two")}
	m.composer.textarea.SetValue("draft")

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !cancelled || m.turn.current != nil {
		t.Fatal("Esc must cancel the turn")
	}
	if got := m.composer.textarea.Value(); got != "one\ntwo\ndraft" || m.queue.hasState() {
		t.Fatalf("composer=%q queue=%+v", got, m.queue.prompts)
	}
}

func TestEscRestoresUntakenSteersWithQueue(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	m.queue.prompts = []queuedPrompt{qp("queued")}
	m.steersToQueue() // no steers: unchanged
	m.turn.current.steers.push(qp("steer"))
	m.tryInterrupt()
	if got := m.composer.textarea.Value(); got != "steer\nqueued" {
		t.Fatalf("composer=%q", got)
	}
}

func TestTurnDoneSendsUntakenSteersFirst(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	m.turn.current.steers.push(qp("late steer"))
	m.queue.prompts = []queuedPrompt{qp("queued")}
	if cmd := m.handleTurnDone(); cmd == nil {
		t.Fatal("expected submit cmd")
	}
	h := m.session.History
	if len(h) != 1 || h[0].Text != "late steer" || len(m.queue.prompts) != 1 {
		t.Fatalf("history=%+v queue=%+v", h, m.queue.prompts)
	}
}

func TestTurnDoneSendsQueuedOneAtATime(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	m.queue.prompts = []queuedPrompt{qp("first"), qp("second")}
	if cmd := m.handleTurnDone(); cmd == nil {
		t.Fatal("expected submit cmd")
	}
	h := m.session.History
	if len(h) != 1 || h[0].Text != "first" || len(m.queue.prompts) != 1 || m.queue.prompts[0].text != "second" {
		t.Fatalf("history=%+v queue=%+v", h, m.queue.prompts)
	}
}

func TestIdleEmptyEnterSendsOldestQueued(t *testing.T) {
	m := testModelWithClient()
	m.queue.prompts = []queuedPrompt{qp("a"), qp("b")}
	if cmd := m.submitInput(); cmd == nil {
		t.Fatal("expected submit cmd")
	}
	if len(m.queue.prompts) != 1 || len(m.session.History) != 1 {
		t.Fatalf("queue=%+v history=%+v", m.queue.prompts, m.session.History)
	}
}

func TestSendRestoresQueueWhenSubmitRefused(t *testing.T) {
	m := testModel() // no client
	m.queue.prompts = []queuedPrompt{qp("a"), qp("b")}
	_ = m.sendQueued()
	if len(m.queue.prompts) != 2 || m.queue.prompts[0].text != "a" {
		t.Fatalf("queue=%+v", m.queue.prompts)
	}
}

func TestAltUpDownWalkQueueWithoutRemoving(t *testing.T) {
	m := testModelWithClient()
	var cancelled bool
	busyTurn(m, &cancelled)
	m.turn.current.steers = &steerBox{}
	m.queue.prompts = []queuedPrompt{qp("one"), qp("two"), qp("three")}
	up := tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt}
	down := tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt}

	m.Update(up)
	if m.composer.textarea.Value() != "three" || len(m.queue.prompts) != 3 {
		t.Fatalf("input=%q queue=%+v", m.composer.textarea.Value(), m.queue.prompts)
	}
	m.Update(up)
	if m.composer.textarea.Value() != "two" {
		t.Fatalf("input=%q", m.composer.textarea.Value())
	}
	m.Update(down)
	if m.composer.textarea.Value() != "three" {
		t.Fatalf("alt+down input=%q", m.composer.textarea.Value())
	}
	m.Update(down)
	if m.composer.textarea.Value() != "" {
		t.Fatalf("past newest should empty the input, got %q", m.composer.textarea.Value())
	}
	m.Update(up)
	m.Update(up)
	m.composer.textarea.SetValue("two!")
	m.Update(up) // edited: left alone
	if m.composer.textarea.Value() != "two!" {
		t.Fatalf("input=%q", m.composer.textarea.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Text: "tab"})
	got := []string{}
	for _, p := range m.queue.prompts {
		got = append(got, p.text)
	}
	if strings.Join(got, ",") != "one,two,three,two!" {
		t.Fatalf("queue=%v", got)
	}
}

func TestRenderFollowups(t *testing.T) {
	m := testModel()
	m.term.width = 100
	m.turn.current = &turnSession{steers: &steerBox{}, ch: closedAgentEvents(), cancel: func() {}, activeTool: -1}
	m.turn.current.steers.push(qp("steer me"))
	m.queue.prompts = []queuedPrompt{qp("hello"), qp("world")}
	out := ansi.Strip(m.renderQueueFollowups(100))
	for _, want := range []string{
		"Steering", "esc interrupts and sends now", "↳ steer me",
		"Queued", "sent when the turn ends", "↳ hello", "↳ world", "alt+↑/↓ edit",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.ContainsAny(out, "│┌└─") {
		t.Fatalf("render has a border: %q", out)
	}
}
