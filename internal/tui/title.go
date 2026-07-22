package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
)

type sessionTitleMsg struct {
	name string
	err  error
}

func requestSessionTitle(client *ai.Client, prompt string) tea.Cmd {
	if client == nil || prompt == "" {
		return nil
	}
	return func() tea.Msg {
		name, err := client.SessionTitle(context.Background(), prompt)
		return sessionTitleMsg{name: name, err: err}
	}
}
