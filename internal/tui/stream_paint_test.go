package tui

import (
	"github.com/axispx/zeta/internal/tools"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/session"
)

func TestTurnDeltaSetsFramePlanInPlanMode(t *testing.T) {
	m := testModel()
	m.session.Mode = prompt.ModePlan
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	next, _ := m.Update(turnDeltaMsg{text: "hi"})
	m = next.(Model)
	if len(m.transcript.messages) != 1 || !m.transcript.messages[0].framePlan {
		t.Fatalf("plan mode agent row should set framePlan: %+v", m.transcript.messages)
	}

	m2 := testModel()
	m2.session.Mode = prompt.ModeBuild
	m2.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
	next, _ = m2.Update(turnDeltaMsg{text: "hi"})
	m2 = next.(Model)
	if len(m2.transcript.messages) != 1 || m2.transcript.messages[0].framePlan {
		t.Fatalf("build mode must not set framePlan: %+v", m2.transcript.messages)
	}
}

func TestAssistantPersistsFramePlan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	for _, tc := range []struct {
		mode prompt.Mode
		want bool
	}{
		{prompt.ModePlan, true},
		{prompt.ModeBuild, false},
		{prompt.ModeAsk, false},
	} {
		t.Run(tc.mode.String(), func(t *testing.T) {
			sess, err := session.New(proj)
			if err != nil {
				t.Fatal(err)
			}
			m := testModel()
			m.session.Log = sess
			m.session.Mode = tc.mode
			m.turn.current = &turnSession{
				cancel:     func() {},
				ch:         closedAgentEvents(),
				activeTool: -1,
			}
			next, _ := m.Update(turnAssistantMsg{
				message: ai.Message{Role: ai.RoleAssistant, Text: "hello"},
			})
			m = next.(Model)

			_, recs, err := session.OpenID(proj, sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(recs) != 1 {
				t.Fatalf("recs=%+v", recs)
			}
			if recs[0].FramePlan != tc.want {
				t.Fatalf("FramePlan=%v want %v", recs[0].FramePlan, tc.want)
			}
			ui, _ := loadSession(recs)
			if len(ui) != 1 || ui[0].framePlan != tc.want {
				t.Fatalf("ui framePlan want %v, ui=%+v", tc.want, ui)
			}
		})
	}
}

func TestRequestStreamPaintCoalesces(t *testing.T) {
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	m.transcript.contentW = 60
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}

	cmd1 := m.requestStreamPaint()
	if cmd1 == nil {
		t.Fatal("first request should schedule a tick")
	}
	if !m.transcript.paint.scheduled {
		t.Fatal("expected paint.scheduled")
	}
	cmd2 := m.requestStreamPaint()
	if cmd2 != nil {
		t.Fatal("second request should not schedule another tick")
	}
	if m.transcript.paint.gen != 0 {
		t.Fatalf("paint.gen=%d, want 0 until cancel", m.transcript.paint.gen)
	}
}

func TestStreamPaintMsgRedrawsAccumulatedDeltas(t *testing.T) {
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	m.transcript.contentW = 60
	m.layout()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}

	next, _ := m.Update(turnDeltaMsg{text: "Hel"})
	m = next.(Model)
	next, _ = m.Update(turnDeltaMsg{text: "lo"})
	m = next.(Model)

	if !m.transcript.paint.scheduled {
		t.Fatal("expected scheduled paint after deltas")
	}
	// Viewport should not show text until paint fires (no immediate refresh).
	if strings.Contains(m.transcript.viewport.GetContent(), "Hello") {
		t.Fatal("expected no paint before streamPaintMsg")
	}
	n := len(m.transcript.messages)
	if n == 0 || m.transcript.messages[n-1].Text != "Hello" {
		t.Fatalf("messages should accumulate: %+v", m.transcript.messages)
	}

	gen := m.transcript.paint.gen
	m.handleStreamPaint(streamPaintMsg{gen: gen})
	if m.transcript.paint.scheduled {
		t.Fatal("paint.scheduled should clear after handle")
	}
	if !strings.Contains(stripANSI(m.transcript.viewport.GetContent()), "Hello") {
		t.Fatalf("missing painted text: %q", stripANSI(m.transcript.viewport.GetContent()))
	}
}

func TestStreamPaintThinkingThrottled(t *testing.T) {
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	m.transcript.contentW = 60
	m.transcript.messages = []Message{{Role: RoleUser, Text: "think"}}
	m.layout()
	m.setTranscriptContent()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}

	next, _ := m.Update(turnReasoningMsg{text: "step-a"})
	m = next.(Model)
	next, _ = m.Update(turnReasoningMsg{text: "\nstep-b"})
	m = next.(Model)

	if m.turn.current.thinking != "step-a\nstep-b" {
		t.Fatalf("thinking=%q", m.turn.current.thinking)
	}
	if strings.Contains(m.transcript.viewport.GetContent(), "step-b") {
		t.Fatal("thinking should not paint before tick")
	}

	m.handleStreamPaint(streamPaintMsg{gen: m.transcript.paint.gen})
	if !strings.Contains(stripANSI(m.transcript.viewport.GetContent()), "step-b") {
		t.Fatalf("missing thinking after paint: %q", stripANSI(m.transcript.viewport.GetContent()))
	}
}

func TestStreamPaintToolOutThrottled(t *testing.T) {
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	m.transcript.contentW = 60
	m.transcript.messages = []Message{
		{Role: RoleUser, Text: "run"},
		{Role: RoleTool, Text: "bash ls", Tool: tools.Bash},
	}
	m.layout()
	m.setTranscriptContent()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: 1,
	}

	next, _ := m.Update(turnToolOutMsg{text: "line1\n", name: tools.Bash})
	m = next.(Model)
	next, _ = m.Update(turnToolOutMsg{text: "line1\nline2\n", name: tools.Bash})
	m = next.(Model)

	if m.transcript.messages[1].Out != "line1\nline2\n" {
		t.Fatalf("out=%q", m.transcript.messages[1].Out)
	}
	if !m.transcript.paint.scheduled {
		t.Fatal("expected throttled paint for tool out")
	}
	if strings.Contains(m.transcript.viewport.GetContent(), "line2") {
		t.Fatal("tool out should not paint before tick")
	}

	m.handleStreamPaint(streamPaintMsg{gen: m.transcript.paint.gen})
	if !strings.Contains(stripANSI(m.transcript.viewport.GetContent()), "line2") {
		t.Fatalf("missing tool out after paint: %q", stripANSI(m.transcript.viewport.GetContent()))
	}
}

func TestCancelStreamPaintIgnoresStaleTick(t *testing.T) {
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	m.transcript.contentW = 60
	m.layout()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}

	next, _ := m.Update(turnDeltaMsg{text: "partial"})
	m = next.(Model)
	staleGen := m.transcript.paint.gen
	m.cancelStreamPaint()
	m.transcript.messages = append(m.transcript.messages, Message{Role: RoleUser, Text: "other"})
	m.repaintTranscript()
	before := m.transcript.viewport.GetContent()

	m.handleStreamPaint(streamPaintMsg{gen: staleGen})
	if m.transcript.viewport.GetContent() != before {
		t.Fatal("stale streamPaintMsg should be ignored")
	}
}

func TestStreamPaintStaleAcrossTurns(t *testing.T) {
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	m.transcript.contentW = 60
	m.layout()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}

	_ = m.requestStreamPaint()
	staleGen := m.transcript.paint.gen
	m.finishTurn()

	// New turn must not honor the previous turn's tick (gen lives on Model).
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
		streaming:  true,
	}
	m.transcript.messages = []Message{{Role: RoleAgent, Text: "next-turn"}}
	m.repaintTranscript()
	before := m.transcript.viewport.GetContent()

	m.handleStreamPaint(streamPaintMsg{gen: staleGen})
	if m.transcript.viewport.GetContent() != before {
		t.Fatal("stale paint from prior turn must not redraw")
	}
	if m.transcript.paint.gen <= staleGen {
		t.Fatalf("finishTurn should bump paint.gen; gen=%d stale=%d", m.transcript.paint.gen, staleGen)
	}
}

func TestAssistantFlushesStreamPaint(t *testing.T) {
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	m.transcript.contentW = 60
	m.layout()
	m.turn.current = &turnSession{
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}

	next, _ := m.Update(turnDeltaMsg{text: "Final answer"})
	m = next.(Model)
	if strings.Contains(m.transcript.viewport.GetContent(), "Final answer") {
		t.Fatal("should still be buffered pre-assistant")
	}

	next, _ = m.Update(turnAssistantMsg{
		message: ai.Message{Role: ai.RoleAssistant, Text: "Final answer"},
	})
	m = next.(Model)
	if !strings.Contains(stripANSI(m.transcript.viewport.GetContent()), "Final answer") {
		t.Fatalf("assistant should flush paint: %q", stripANSI(m.transcript.viewport.GetContent()))
	}
	if m.transcript.paint.scheduled {
		t.Fatal("pending paint should be cancelled on flush")
	}
}

func TestRefreshTranscriptCancelsPendingPaint(t *testing.T) {
	m := testModel()
	m.turn.current = &turnSession{cancel: func() {}, ch: closedAgentEvents(), activeTool: -1}
	_ = m.requestStreamPaint()
	stale := m.transcript.paint.gen
	m.refreshTranscript()
	if m.transcript.paint.scheduled {
		t.Fatal("refreshTranscript should clear scheduled")
	}
	if m.transcript.paint.gen <= stale {
		t.Fatal("refreshTranscript should bump gen")
	}
}
