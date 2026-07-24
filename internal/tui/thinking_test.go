package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/ai"
)

func closedAgentEvents() <-chan agent.Event {
	ch := make(chan agent.Event)
	close(ch)
	return ch
}

func TestRenderThinkingTailClipsToFourLines(t *testing.T) {
	in := "one\ntwo\nthree\nfour\nfive\nsix"
	out := stripANSI(renderThinkingTail(in, 40))
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("lines=%d: %q", len(lines), out)
	}
	if strings.TrimSpace(lines[0]) != "three" || strings.TrimSpace(lines[3]) != "six" {
		t.Fatalf("tail=%q", out)
	}
}

func TestRenderThinkingTailWrapsThenClips(t *testing.T) {
	in := strings.Repeat("abcdefghij", 8) // 80 chars → 2 lines at width 40
	out := stripANSI(renderThinkingTail(in+"\nline2\nline3\nline4\nline5", 40))
	if h := lipgloss.Height(out); h != 4 {
		t.Fatalf("height=%d: %q", h, out)
	}
	if strings.Contains(out, strings.Repeat("abcdefghij", 5)) {
		t.Fatalf("should clip oldest wrapped lines: %q", out)
	}
}

func TestRenderThinkingTailEmpty(t *testing.T) {
	if got := renderThinkingTail("", 40); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := renderThinkingTail("\n\n", 40); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestAppendThinkingBounds(t *testing.T) {
	// Fill well past the cap; only the recent tail should remain.
	chunk := strings.Repeat("x", 500)
	var got string
	for range 10 {
		got = appendThinking(got, chunk)
	}
	if len(got) > maxThinkingBytes {
		t.Fatalf("len=%d, want <= %d", len(got), maxThinkingBytes)
	}
	if !strings.HasSuffix(got, "xxx") {
		t.Fatalf("expected recent tail retained")
	}
	// Prefix from early chunks must be gone.
	if len(got) == 10*500 {
		t.Fatal("buffer was not clipped")
	}
}

func TestThinkingLifecycleClearsOnDelta(t *testing.T) {
	m := testModel()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}

	next, cmd := m.Update(turnReasoningMsg{text: "step1\n"})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected wait cmd")
	}
	if m.turn.thinking != "step1\n" {
		t.Fatalf("thinking=%q", m.turn.thinking)
	}
	next, _ = m.Update(turnReasoningMsg{text: "step2"})
	m = next.(Model)
	if m.turn.thinking != "step1\nstep2" {
		t.Fatalf("thinking=%q", m.turn.thinking)
	}

	next, _ = m.Update(turnDeltaMsg{text: "Hello"})
	m = next.(Model)
	if m.turn.thinking != "" {
		t.Fatalf("thinking not cleared: %q", m.turn.thinking)
	}
	if !m.turn.streaming {
		t.Fatal("expected streaming")
	}
	n := len(m.messages)
	if n == 0 || m.messages[n-1].Role != RoleAgent || m.messages[n-1].Text != "Hello" {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestThinkingIgnoredWhileStreaming(t *testing.T) {
	m := testModel()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		streaming:  true,
		thinking:   "",
		activeTool: -1,
	}
	next, _ := m.Update(turnReasoningMsg{text: "nope"})
	m = next.(Model)
	if m.turn.thinking != "" {
		t.Fatalf("thinking=%q", m.turn.thinking)
	}
}

func TestThinkingClearsOnToolStart(t *testing.T) {
	m := testModel()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		thinking:   "ponder",
		activeTool: -1,
	}
	next, _ := m.Update(turnToolStartMsg{label: "read a.go", name: "read"})
	m = next.(Model)
	if m.turn.thinking != "" {
		t.Fatalf("thinking=%q", m.turn.thinking)
	}
	if m.turn.activeTool < 0 {
		t.Fatal("expected active tool")
	}
}

func TestThinkingClearsOnAssistant(t *testing.T) {
	m := testModel()
	m.turn = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		thinking:   "ponder",
		activeTool: -1,
	}
	next, _ := m.Update(turnAssistantMsg{
		message: ai.Message{Role: ai.RoleAssistant, Text: ""},
	})
	m = next.(Model)
	if m.turn.thinking != "" {
		t.Fatalf("thinking=%q", m.turn.thinking)
	}
}

func TestThinkingPhase(t *testing.T) {
	var nilTurn *turnSession
	if nilTurn.thinkingPhase() {
		t.Fatal("nil turn should not be thinking phase")
	}
	t0 := &turnSession{activeTool: -1}
	if !t0.thinkingPhase() {
		t.Fatal("fresh turn should be thinking phase")
	}
	t0.streaming = true
	if t0.thinkingPhase() {
		t.Fatal("streaming should end thinking phase")
	}
	t0.streaming = false
	t0.activeTool = 0
	if t0.thinkingPhase() {
		t.Fatal("active tool should end thinking phase")
	}
}

func TestAcceptReasoningAndBeginStreaming(t *testing.T) {
	var nilTurn *turnSession
	if nilTurn.acceptReasoning("x") {
		t.Fatal("nil turn should reject reasoning")
	}

	t0 := &turnSession{activeTool: -1}
	if !t0.acceptReasoning("a") || t0.thinking != "a" {
		t.Fatalf("thinking=%q", t0.thinking)
	}
	if !t0.acceptReasoning("b") || t0.thinking != "ab" {
		t.Fatalf("thinking=%q", t0.thinking)
	}

	t0.beginStreaming()
	if !t0.streaming || t0.thinking != "" {
		t.Fatalf("after beginStreaming: streaming=%v thinking=%q", t0.streaming, t0.thinking)
	}
	if t0.acceptReasoning("nope") {
		t.Fatal("should ignore reasoning while streaming")
	}
	if !t0.endStreaming() {
		t.Fatal("streaming segment should report dirty")
	}
	if t0.streaming {
		t.Fatal("expected streaming false after endStreaming")
	}
	if t0.endStreaming() {
		t.Fatal("second endStreaming should be clean")
	}

	// Pure-reasoning path: never streamed, but thinking must still dirty.
	t1 := &turnSession{activeTool: -1, thinking: "hmm"}
	if !t1.endStreaming() || t1.thinking != "" {
		t.Fatalf("thinking-only end: dirty thinking=%q", t1.thinking)
	}
}
