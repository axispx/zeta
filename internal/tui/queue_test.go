package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/image"
)

func testModelWithClient() Model {
	m := testModel()
	m.cfg = testClientCfg()
	m.applyClient()
	return m
}

func offeredPrompt(text string) *queuedPrompt {
	p := queuedPrompt{text: text, display: text}
	return &p
}

func TestEnqueueDuringTurn(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     make(chan ai.Message, 1),
		activeTool: -1,
	}
	m.textarea.SetValue("follow up")

	cmd := m.submitInput()
	if cmd != nil {
		t.Fatal("enqueue should not start turn")
	}
	if len(m.queue) != 1 || m.queue[0].text != "follow up" {
		t.Fatalf("queue=%+v", m.queue)
	}
	if len(m.history) != 0 || len(m.messages) != 0 {
		t.Fatalf("history=%v messages=%v", m.history, m.messages)
	}
	if m.textarea.Value() != "" {
		t.Fatalf("input=%q", m.textarea.Value())
	}
}

func TestPromoteOldestOnEmptyEnter(t *testing.T) {
	m := testModelWithClient()
	steer := make(chan ai.Message, 1)
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     steer,
		activeTool: -1,
	}
	m.queue = []queuedPrompt{{text: "steer this", display: "steer this"}}

	_ = m.submitInput()

	if m.offered == nil || m.offered.text != "steer this" {
		t.Fatalf("promote should set offered: offered=%v", m.offered)
	}
	if len(m.queue) != 0 {
		t.Fatalf("promote removes from queue: queue=%+v", m.queue)
	}
	select {
	case msg := <-steer:
		if msg.Text != "steer this" {
			t.Fatalf("steer msg=%q", msg.Text)
		}
	default:
		t.Fatal("expected message on steer channel")
	}
}

func TestSteerAcceptedCommitsOnce(t *testing.T) {
	m := testModelWithClient()
	m.offered = offeredPrompt("steered")
	m.turn = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}

	cmd := m.handleTurnSteerAccepted(ai.Message{Role: ai.RoleUser, Text: "steered"})
	if cmd == nil {
		t.Fatal("expected waitTurn cmd")
	}
	if m.offered != nil || len(m.queue) != 0 {
		t.Fatalf("after ack: offered=%v queue=%+v", m.offered, m.queue)
	}
	if len(m.history) != 1 || m.history[0].Text != "steered" {
		t.Fatalf("history=%+v", m.history)
	}
	if len(m.messages) != 1 || m.messages[0].Text != "steered" {
		t.Fatalf("messages=%+v", m.messages)
	}
}

// Esc may clear offered before KindSteerAccepted is processed; agent already has the msg.
func TestSteerAcceptedCommitsEvenIfOfferedCleared(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}
	// offered already nil (Esc race), but agent accepted.
	cmd := m.handleTurnSteerAccepted(ai.Message{Role: ai.RoleUser, Text: "still commit"})
	if cmd == nil {
		t.Fatal("expected waitTurn")
	}
	if len(m.history) != 1 || m.history[0].Text != "still commit" {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestDrainRestoresWhenSubmitRefused(t *testing.T) {
	m := testModel() // no client
	m.queue = []queuedPrompt{{text: "keep me", display: "keep me"}}
	cmd := m.drainNextQueuedPrompt()
	if cmd != nil {
		t.Fatal("expected nil cmd when no client")
	}
	if len(m.queue) != 1 || m.queue[0].text != "keep me" {
		t.Fatalf("prompt must be restored: queue=%+v", m.queue)
	}
	if len(m.history) != 0 {
		t.Fatalf("must not commit: history=%+v", m.history)
	}
}

func TestDrainRestoresOfferedWhenSubmitRefused(t *testing.T) {
	m := testModel() // no client
	m.offered = offeredPrompt("promoted")
	cmd := m.drainNextQueuedPrompt()
	if cmd != nil {
		t.Fatal("expected nil")
	}
	// Offered becomes queue head (no active turn.steers after complete).
	if m.offered != nil || len(m.queue) != 1 || m.queue[0].text != "promoted" {
		t.Fatalf("offered=%v queue=%+v", m.offered, m.queue)
	}
}

func TestTurnDoneDrainsQueueFIFO(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{
		{text: "first", display: "first"},
		{text: "second", display: "second"},
	}
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

func TestTurnDoneUsesOfferedBeforeQueue(t *testing.T) {
	m := testModelWithClient()
	// Active turn: complete must not drop unconsumed offer before drain.
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     make(chan ai.Message, 1),
		activeTool: -1,
	}
	m.offered = offeredPrompt("steered")
	m.queue = []queuedPrompt{{text: "queued", display: "queued"}}
	cmd := m.handleTurnDone()
	if cmd == nil {
		t.Fatal("expected submit")
	}
	// Unconsumed offer becomes a fresh turn (not dropped by complete).
	if m.offered != nil {
		t.Fatal("offered should clear on drain")
	}
	if m.turn == nil {
		t.Fatal("drain should start a new turn for the unconsumed offer")
	}
	if len(m.queue) != 1 || m.queue[0].text != "queued" {
		t.Fatalf("queue=%+v", m.queue)
	}
	if len(m.history) != 1 || m.history[0].Text != "steered" {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestCancelAbandonsOffered(t *testing.T) {
	m := testModelWithClient()
	steer := make(chan ai.Message, 1)
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     steer,
		activeTool: -1,
	}
	m.offered = offeredPrompt("drop")
	m.queue = []queuedPrompt{{text: "keep", display: "keep"}}
	steer <- ai.Message{Role: ai.RoleUser, Text: "drop"}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.offered != nil {
		t.Fatal("cancel should abandon offer")
	}
	if len(m.queue) != 1 || m.queue[0].text != "keep" {
		t.Fatalf("queue should keep remaining FIFO items: %+v", m.queue)
	}
	if len(m.history) != 0 {
		t.Fatalf("history should stay empty: %+v", m.history)
	}
}

func TestLateDoneAfterCancelDoesNotDrainQueue(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     make(chan ai.Message, 1),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{
		{text: "keep", display: "keep"},
		{text: "also", display: "also"},
	}

	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.turn != nil {
		t.Fatal("turn should be cleared")
	}
	if len(m.queue) != 2 {
		t.Fatalf("queue should remain after cancel: %+v", m.queue)
	}

	// Spurious KindDone from the cancelled agent must not auto-start follow-ups.
	cmd := m.handleTurnDone()
	if cmd != nil {
		t.Fatal("late Done after cancel must not drain queue")
	}
	if len(m.queue) != 2 {
		t.Fatalf("queue=%+v", m.queue)
	}
	if len(m.history) != 0 {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestEscDiscardsOldestQueued(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		steers:     make(chan ai.Message, 1),
		activeTool: -1,
	}
	m.queue = []queuedPrompt{
		{text: "drop", display: "drop"},
		{text: "keep", display: "keep"},
	}

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	mm := next.(Model)
	if cancelled {
		t.Fatal("turn should not cancel")
	}
	if len(mm.queue) != 1 || mm.queue[0].text != "keep" {
		t.Fatalf("queue=%+v", mm.queue)
	}
	if mm.turn == nil {
		t.Fatal("turn should remain active")
	}
}

func TestEscCancelsOfferedWithoutSubmit(t *testing.T) {
	m := testModelWithClient()
	steer := make(chan ai.Message, 1)
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     steer,
		activeTool: -1,
	}
	m.offered = offeredPrompt("promoted")
	steer <- ai.Message{Role: ai.RoleUser, Text: "promoted"}

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	mm := next.(Model)
	if mm.offered != nil || len(mm.queue) != 0 {
		t.Fatalf("offer should clear on esc: offered=%v queue=%+v", mm.offered, mm.queue)
	}
	if len(mm.history) != 0 {
		t.Fatalf("history=%v", mm.history)
	}
	select {
	case <-steer:
		t.Fatal("steer channel should be empty")
	default:
	}
	cmd := mm.submitInput()
	if cmd != nil || len(mm.history) != 0 {
		t.Fatalf("empty enter after esc should not submit, history=%v", mm.history)
	}
}

func TestEscAfterQueueEmptyCancelsTurn(t *testing.T) {
	m := testModelWithClient()
	cancelled := false
	m.turn = &turnSession{
		cancel:     func() { cancelled = true },
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	mm := next.(Model)
	if !cancelled || mm.turn != nil {
		t.Fatal("expected turn cancelled")
	}
}

func TestIdleEmptyEnterDrainsQueue(t *testing.T) {
	m := testModelWithClient()
	m.queue = []queuedPrompt{{text: "next", display: "next"}}
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

func TestEnqueueWithImages(t *testing.T) {
	isolateZetaHome(t)
	m, err := New(testClientCfg(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     make(chan ai.Message, 1),
		activeTool: -1,
	}
	m.insertImageAttach(testAttach("a.png"))
	_ = m.enqueuePrompt("look", []image.Ref{{URL: testPNGDataURL, Name: "a.png"}})
	if len(m.queue) != 1 || len(m.queue[0].imgs) != 1 {
		t.Fatalf("queue=%+v", m.queue)
	}
}

func TestSlashHarnessBlockedDuringTurn(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     make(chan ai.Message, 1),
		activeTool: -1,
	}
	m.textarea.SetValue("/clear")
	cmd := m.submitInput()
	if cmd != nil {
		t.Fatal("harness command should not run during turn")
	}
	if len(m.queue) != 0 {
		t.Fatalf("queue=%+v", m.queue)
	}
}

func TestSlashUnknownBlockedDuringTurn(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     make(chan ai.Message, 1),
		activeTool: -1,
	}
	m.textarea.SetValue("/not-a-command")
	cmd := m.submitInput()
	if cmd != nil {
		t.Fatal("unknown slash should not run during turn")
	}
	if len(m.queue) != 0 {
		t.Fatalf("queue=%+v", m.queue)
	}
	if len(m.messages) != 0 {
		t.Fatalf("no error toast mid-turn: messages=%+v", m.messages)
	}
	if m.textarea.Value() != "/not-a-command" {
		t.Fatalf("input should stay: %q", m.textarea.Value())
	}
}

func TestSlashSkillQueuesDuringTurn(t *testing.T) {
	m := testModelWithClient()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     make(chan ai.Message, 1),
		activeTool: -1,
	}
	m.textarea.SetValue("/review args")
	_ = m.submitInput()
	if len(m.queue) != 1 || !strings.HasPrefix(m.queue[0].text, "/review") {
		t.Fatalf("queue=%+v", m.queue)
	}
}

func TestClearQueueOnApplySession(t *testing.T) {
	m := testModel()
	m.queue = []queuedPrompt{{text: "x", display: "x"}, {text: "y", display: "y"}}
	m.offered = offeredPrompt("z")
	m.applySession(nil, nil, nil)
	if m.hasQueueState() || m.offered != nil {
		t.Fatal("queue should clear on session apply")
	}
}

func TestModeSwitchBlockedWithQueue(t *testing.T) {
	m := testModel()
	m.queue = []queuedPrompt{{text: "x", display: "x"}}
	before := m.mode
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift, Text: "shift+tab"})
	mm := next.(Model)
	if mm.mode != before {
		t.Fatal("mode should not change with pending queue")
	}
}

func TestModeSwitchBlockedWithOffered(t *testing.T) {
	m := testModel()
	m.offered = offeredPrompt("x")
	before := m.mode
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift, Text: "shift+tab"})
	mm := next.(Model)
	if mm.mode != before {
		t.Fatal("mode should not change with in-flight offer")
	}
}

func TestPromoteNonBlockingWhenChannelFull(t *testing.T) {
	m := testModelWithClient()
	steer := make(chan ai.Message) // unbuffered → send would block without select
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		steers:     steer,
		activeTool: -1,
	}
	m.queue = []queuedPrompt{{text: "a", display: "a"}}
	m.promoteOldestToSteer()
	if m.offered != nil || len(m.queue) != 1 {
		t.Fatalf("full channel must leave queue intact: offered=%v queue=%+v", m.offered, m.queue)
	}
}

func TestRenderQueueFollowupsLayout(t *testing.T) {
	m := testModel()
	m.turn = &turnSession{cancel: func() {}, activeTool: -1}
	m.queue = []queuedPrompt{{text: "a", display: "a"}}

	plain := ansi.Strip(m.renderQueueFollowups(80))
	for _, want := range []string{
		"follow-ups",
		"○ a",
		"enter send now",
		"esc cancel",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "select/edit") {
		t.Fatal("queue panel must not advertise unimplemented select/edit")
	}
	// Hints on their own row at the bottom of the panel body.
	lines := strings.Split(plain, "\n")
	var itemLine, hintLine string
	for _, line := range lines {
		if strings.Contains(line, "○ a") {
			itemLine = line
		}
		if strings.Contains(line, "enter send now") {
			hintLine = line
		}
	}
	if itemLine == "" || hintLine == "" {
		t.Fatalf("missing item or hint line:\n%s", plain)
	}
	if strings.Contains(itemLine, "enter send now") {
		t.Fatalf("hints should not share item row:\n%s", itemLine)
	}
	if strings.Contains(hintLine, "○ a") {
		t.Fatalf("hints should not share item row:\n%s", hintLine)
	}
}

func TestRenderFollowupsShowsOfferedFirst(t *testing.T) {
	m := testModel()
	m.offered = offeredPrompt("promoted")
	m.queue = []queuedPrompt{{text: "waiting", display: "waiting"}}
	plain := ansi.Strip(m.renderQueueFollowups(80))
	pi := strings.Index(plain, "promoted")
	wi := strings.Index(plain, "waiting")
	if pi < 0 || wi < 0 || pi > wi {
		t.Fatalf("promoted should render before waiting:\n%s", plain)
	}
}
