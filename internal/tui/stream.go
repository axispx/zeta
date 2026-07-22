package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/workspace"
)

type streamSession struct {
	cancel context.CancelFunc
	ch     <-chan ai.Event
}

type streamDeltaMsg struct{ text string }
type streamDoneMsg struct{}
type streamErrMsg struct{ err error }

func toAIMessages(ws workspace.Context, mode prompt.Mode, msgs []Message) []ai.Message {
	out := make([]ai.Message, 0, len(msgs)+2)
	out = append(out,
		ai.Message{Role: ai.RoleSystem, Text: prompt.System(ws)},
		ai.Message{Role: ai.RoleDeveloper, Text: mode.Instructions()},
	)
	for _, m := range msgs {
		switch m.Role {
		case RoleUser:
			out = append(out, ai.Message{Role: ai.RoleUser, Text: m.Text})
		case RoleAgent:
			out = append(out, ai.Message{Role: ai.RoleAssistant, Text: m.Text})
		}
	}
	return out
}

func waitStreamEvent(ch <-chan ai.Event) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		switch evt.Type {
		case ai.EventDelta:
			return streamDeltaMsg{text: evt.Text}
		case ai.EventDone:
			return streamDoneMsg{}
		case ai.EventErr:
			return streamErrMsg{err: evt.Err}
		default:
			return streamDoneMsg{}
		}
	}
}

func startStream(client *ai.Client, ws workspace.Context, mode prompt.Mode, msgs []Message) (*streamSession, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := client.Stream(ctx, toAIMessages(ws, mode, msgs))
	return &streamSession{cancel: cancel, ch: ch}, waitStreamEvent(ch)
}
