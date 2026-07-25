package tui

import (
	"context"
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/skill"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
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

// turnSession is one in-flight agent turn (stream + tool loop).
type turnSession struct {
	cancel     context.CancelFunc
	ch         <-chan agent.Event
	reply      chan<- agent.Reply // harness → agent; one decision per gated start
	streaming  bool               // true while receiving assistant deltas
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

type turnDeltaMsg struct{ text string }
type turnReasoningMsg struct{ text string }
type turnAssistantMsg struct {
	message ai.Message
	usage   ai.Usage
}
type turnToolStartMsg struct {
	label  string
	name   string
	path   string
	detail string
	args   json.RawMessage // raw tool args (interactive tools)
}
type turnToolOutMsg struct {
	text string
	name string
}
type turnToolMsg struct {
	label   string
	name    string
	message ai.Message
	denied  bool
}
type turnDoneMsg struct{}
type turnErrMsg struct{ err error }

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

func toolsForMode(mode prompt.Mode) []tools.Tool {
	switch mode {
	case prompt.ModeAsk, prompt.ModePlan:
		return tools.Inspect()
	default:
		return tools.Build()
	}
}

// requestMsgs prepends system + mode instructions to the durable history
// and expands a trailing slash-skill user turn into a developer playbook.
// Durable history keeps the user text (token + optional args); completed
// slash turns are not re-injected on later requests.
func requestMsgs(ws workspace.Context, mode prompt.Mode, history []ai.Message) []ai.Message {
	out := make([]ai.Message, 0, len(history)+3)
	out = append(out,
		ai.Message{Role: ai.RoleSystem, Text: prompt.System(ws)},
		ai.Message{Role: ai.RoleDeveloper, Text: mode.Instructions()},
	)
	out = append(out, history...)
	if n := len(history); n > 0 {
		last := history[n-1]
		if last.Role == ai.RoleUser {
			if s, ok := skill.MatchSlash(last.Text); ok {
				out = append(out, ai.Message{Role: ai.RoleDeveloper, Text: skill.SlashInjection(s)})
			}
		}
	}
	return out
}

func waitTurn(t *turnSession) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-t.ch
		if !ok {
			return turnDoneMsg{}
		}
		return turnEventMsg(evt)
	}
}

func turnEventMsg(evt agent.Event) tea.Msg {
	switch evt.Kind {
	case agent.KindDelta:
		return turnDeltaMsg{text: evt.Text}
	case agent.KindReasoning:
		return turnReasoningMsg{text: evt.Text}
	case agent.KindAssistant:
		return turnAssistantMsg{message: evt.Message, usage: evt.Usage}
	case agent.KindToolStart:
		return turnToolStartMsg{
			label:  evt.Text,
			name:   evt.Name,
			path:   evt.Path,
			detail: evt.Detail,
			args:   evt.Args,
		}
	case agent.KindToolOut:
		return turnToolOutMsg{text: evt.Text, name: evt.Name}
	case agent.KindTool:
		return turnToolMsg{label: evt.Text, name: evt.Name, message: evt.Message, denied: evt.Denied}
	case agent.KindDone:
		return turnDoneMsg{}
	case agent.KindErr:
		return turnErrMsg{err: evt.Err}
	default:
		return turnDoneMsg{}
	}
}

func startTurn(client *ai.Client, ws workspace.Context, mode prompt.Mode, history []ai.Message, grants *permission.Session) (*turnSession, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	replies := make(chan agent.Reply, 1)
	cfg := agent.Config{
		Client:  client,
		Tools:   toolsForMode(mode),
		Root:    ws.Abs,
		Replies: replies,
		// Same classifier as handleTurnToolStart (waitFor).
		Gate: func(name string) bool {
			return waitFor(name, grants) != waitNone
		},
	}
	ch := cfg.Run(ctx, requestMsgs(ws, mode, history))
	t := &turnSession{
		cancel:     cancel,
		ch:         ch,
		reply:      replies,
		streaming:  false, // set true on first delta; false = Waiting chrome / settled md
		activeTool: -1,
	}
	return t, waitTurn(t)
}
