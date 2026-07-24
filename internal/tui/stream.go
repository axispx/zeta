package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

// turnSession is one in-flight agent turn (stream + tool loop).
type turnSession struct {
	cancel     context.CancelFunc
	ch         <-chan agent.Event
	streaming  bool   // true while receiving assistant deltas
	thinking   string // live reasoning tail; only while thinkingPhase
	activeTool int    // index of open tool row in Model.messages; -1 if none
}

// thinkingPhase is true before answer deltas or an open tool (pre-answer reasoning).
func (t *turnSession) thinkingPhase() bool {
	return t != nil && !t.streaming && t.activeTool < 0
}

// acceptReasoning appends a reasoning delta during thinkingPhase.
// Returns whether the live tail changed (caller should refresh the transcript).
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

// endStreaming marks the assistant message as complete (tools may follow).
// Returns whether a live reasoning tail was cleared (needs transcript refresh).
func (t *turnSession) endStreaming() bool {
	if t == nil {
		return false
	}
	t.streaming = false
	if t.thinking == "" {
		return false
	}
	t.thinking = ""
	return true
}

type turnDeltaMsg struct{ text string }
type turnReasoningMsg struct{ text string }
type turnAssistantMsg struct {
	message ai.Message
	usage   ai.Usage
}
type turnToolStartMsg struct {
	label string
	name  string
}
type turnToolOutMsg struct {
	text string
	name string
}
type turnToolMsg struct {
	label   string
	name    string
	message ai.Message
}
type turnDoneMsg struct{}
type turnErrMsg struct{ err error }

func toolsForMode(mode prompt.Mode) []tools.Tool {
	switch mode {
	case prompt.ModeAsk, prompt.ModePlan:
		return tools.Inspect()
	default:
		return tools.Build()
	}
}

// requestMsgs prepends system + mode instructions to the durable history.
func requestMsgs(ws workspace.Context, mode prompt.Mode, history []ai.Message) []ai.Message {
	out := make([]ai.Message, 0, len(history)+2)
	out = append(out,
		ai.Message{Role: ai.RoleSystem, Text: prompt.System(ws)},
		ai.Message{Role: ai.RoleDeveloper, Text: mode.Instructions()},
	)
	return append(out, history...)
}

func waitTurnEvent(ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return turnDoneMsg{}
		}
		switch evt.Kind {
		case agent.KindDelta:
			return turnDeltaMsg{text: evt.Text}
		case agent.KindReasoning:
			return turnReasoningMsg{text: evt.Text}
		case agent.KindAssistant:
			return turnAssistantMsg{message: evt.Message, usage: evt.Usage}
		case agent.KindToolStart:
			return turnToolStartMsg{label: evt.Text, name: evt.Name}
		case agent.KindToolOut:
			return turnToolOutMsg{text: evt.Text, name: evt.Name}
		case agent.KindTool:
			return turnToolMsg{label: evt.Text, name: evt.Name, message: evt.Message}
		case agent.KindDone:
			return turnDoneMsg{}
		case agent.KindErr:
			return turnErrMsg{err: evt.Err}
		default:
			return turnDoneMsg{}
		}
	}
}

func startTurn(client *ai.Client, ws workspace.Context, mode prompt.Mode, history []ai.Message) (*turnSession, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := agent.Config{
		Client: client,
		Tools:  toolsForMode(mode),
		Root:   ws.Abs,
	}
	ch := cfg.Run(ctx, requestMsgs(ws, mode, history))
	return &turnSession{
		cancel:     cancel,
		ch:         ch,
		streaming:  false, // set true on first delta; false = Waiting chrome / settled md
		activeTool: -1,
	}, waitTurnEvent(ch)
}
