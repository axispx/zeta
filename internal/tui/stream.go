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
	cancel    context.CancelFunc
	ch        <-chan agent.Event
	streaming bool // receiving assistant deltas (plain render)
}

type turnDeltaMsg struct{ text string }
type turnAssistantMsg struct {
	message ai.Message
	usage   ai.Usage
}
type turnToolMsg struct {
	label   string
	message ai.Message
}
type turnDoneMsg struct{}
type turnErrMsg struct{ err error }

func toolsForMode(mode prompt.Mode) []tools.Tool {
	switch mode {
	case prompt.ModeAsk, prompt.ModePlan:
		return tools.ReadOnly(tools.All())
	default:
		return tools.All()
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
		case agent.KindAssistant:
			return turnAssistantMsg{message: evt.Message, usage: evt.Usage}
		case agent.KindTool:
			return turnToolMsg{label: evt.Text, message: evt.Message}
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
	return &turnSession{cancel: cancel, ch: ch, streaming: true}, waitTurnEvent(ch)
}
