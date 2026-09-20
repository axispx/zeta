package tui

import (
	"context"
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/core"
)

// streamPaintEvery caps how often live answer/thinking/tool-out redraw the transcript.
const streamPaintEvery = 30 * time.Millisecond

// streamPaint throttles transcript redraws while tokens arrive.
// Lives on Model (not turnSession) so gen survives turn boundaries — a stale
// tick from turn N must not paint turn N+1.
type streamPaint struct {
	gen       int
	scheduled bool
}

// turnDecider delivers harness decisions to the agent over the turn's reply
// channel. Send-only on the turn side, so it takes a bidirectional channel.
type turnDecider struct{ reply chan agent.Reply }

func (d turnDecider) Decide(ctx context.Context, _ agent.Request) (agent.Reply, error) {
	select {
	case r := <-d.reply:
		return r, nil
	case <-ctx.Done():
		return agent.Reply{}, ctx.Err()
	}
}

// turnSession is one in-flight agent turn (stream + tool loop).
type turnSession struct {
	id         int // matches turn*Msg.id; drops late events after cancel/replace
	cancel     context.CancelFunc
	ch         <-chan agent.Event
	reply      chan<- agent.Reply // harness → agent; one decision per gated start
	streaming  bool               // true while receiving assistant deltas
	pending    *agent.Event       // set when coalesce peeks a non-matching event
	thinking   string             // live reasoning tail; only while thinkingPhase
	activeTool int                // index of open tool row in Model.messages; -1 if none
}

// thinkingPhase is true before answer deltas or an open tool (pre-answer reasoning).
func (t *turnSession) thinkingPhase() bool {
	return t != nil && !t.streaming && t.activeTool < 0
}

// acceptReasoning appends a reasoning delta during thinkingPhase.
// Returns whether the live tail changed (caller should schedule a paint).
func (t *turnSession) acceptReasoning(delta string) bool {
	if t == nil || !t.thinkingPhase() || delta == "" {
		return false
	}
	t.thinking = appendThinking(t.thinking, delta)
	return true
}

// beginStreaming marks answer tokens as started and drops any live reasoning tail.
func (t *turnSession) beginStreaming() {
	if t == nil {
		return
	}
	t.streaming = true
	t.thinking = ""
}

// endStreaming marks the assistant segment complete (tools may follow).
// Returns true when the live stream UI was dirty (answer and/or thinking).
func (t *turnSession) endStreaming() bool {
	if t == nil {
		return false
	}
	dirty := t.streaming || t.thinking != ""
	t.streaming = false
	t.thinking = ""
	return dirty
}

type turnDeltaMsg struct {
	id   int
	text string
}
type turnReasoningMsg struct {
	id   int
	text string
}
type turnAssistantMsg struct {
	id      int
	message ai.Message
	usage   ai.Usage
}
type turnToolStartMsg struct {
	id     int
	label  string
	name   string
	path   string
	detail string
	args   json.RawMessage // raw tool args (interactive tools)
}
type turnToolOutMsg struct {
	id   int
	text string
	name string
}
type turnToolMsg struct {
	id      int
	label   string
	name    string
	message ai.Message
	denied  bool
}
type (
	turnDoneMsg struct{ id int }
	turnErrMsg  struct {
		id  int
		err error
	}
)

// streamPaintMsg fires after streamPaintEvery to paint accumulated live text.
type streamPaintMsg struct{ gen int }

// requestStreamPaint schedules a throttled transcript redraw.
// Safe on every delta/tool-out; only one tick is outstanding at a time.
func (m *Model) requestStreamPaint() tea.Cmd {
	if m.paint.scheduled {
		return nil
	}
	m.paint.scheduled = true
	gen := m.paint.gen
	return tea.Tick(streamPaintEvery, func(time.Time) tea.Msg {
		return streamPaintMsg{gen: gen}
	})
}

// cancelStreamPaint drops any pending throttled paint (gen bump invalidates in-flight ticks).
func (m *Model) cancelStreamPaint() {
	m.paint.gen++
	m.paint.scheduled = false
}

// handleStreamPaint applies a due throttled redraw.
func (m *Model) handleStreamPaint(msg streamPaintMsg) {
	if msg.gen != m.paint.gen {
		return // stale after cancel/finish/refresh
	}
	m.paint.scheduled = false
	m.repaintTranscript()
}

func waitTurn(t *turnSession) tea.Cmd {
	id := t.id
	return func() tea.Msg {
		evt, ok := recvTurnEvent(t)
		if !ok {
			return turnDoneMsg{id: id}
		}
		return turnEventMsg(id, evt)
	}
}

func recvTurnEvent(t *turnSession) (agent.Event, bool) {
	// take the pending event if it exists
	if t.pending != nil {
		evt := *t.pending
		t.pending = nil
		return evt, true
	}

	// first event is always blocking
	// or prioritize take pending one above
	evt, ok := <-t.ch
	if !ok {
		return agent.Event{}, false
	}

	// we only coalesce on delta/reasoning text
	if evt.Kind != agent.KindDelta && evt.Kind != agent.KindReasoning {
		return evt, true
	}

	for {
		select {
		case next, ok := <-t.ch:
			// channel closed; return what we have from above
			if !ok {
				return evt, true
			}

			if next.Kind == evt.Kind {
				evt.Text += next.Text
				continue
			}

			// overshoot: stash exactly one
			t.pending = &next
			return evt, true
		default:
			return evt, true
		}
	}
}

func turnEventMsg(id int, evt agent.Event) tea.Msg {
	switch evt.Kind {
	case agent.KindDelta:
		return turnDeltaMsg{id: id, text: evt.Text}
	case agent.KindReasoning:
		return turnReasoningMsg{id: id, text: evt.Text}
	case agent.KindAssistant:
		return turnAssistantMsg{id: id, message: evt.Message, usage: evt.Usage}
	case agent.KindToolStart:
		return turnToolStartMsg{
			id:     id,
			label:  evt.Text,
			name:   evt.Name,
			path:   evt.Path,
			detail: evt.Detail,
			args:   evt.Args,
		}
	case agent.KindToolOut:
		return turnToolOutMsg{id: id, text: evt.Text, name: evt.Name}
	case agent.KindTool:
		return turnToolMsg{id: id, label: evt.Text, name: evt.Name, message: evt.Message, denied: evt.Denied}
	case agent.KindDone:
		return turnDoneMsg{id: id}
	case agent.KindErr:
		return turnErrMsg{id: id, err: evt.Err}
	default:
		return turnDoneMsg{id: id}
	}
}

func startTurn(id int, client *ai.Client, sess *core.Session) (*turnSession, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	replies := make(chan agent.Reply, 1)
	ch := sess.Run(ctx, client, turnDecider{replies})
	t := &turnSession{
		id:         id,
		cancel:     cancel,
		ch:         ch,
		reply:      replies,
		streaming:  false, // set true on first delta; false = Waiting chrome / settled md
		activeTool: -1,
	}
	return t, waitTurn(t)
}
