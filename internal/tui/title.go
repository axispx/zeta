package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/session"
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

// terminalTitle is the OSC window title for the session display name.
// Empty/untitled sessions leave the title blank.
func terminalTitle(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	name := strings.TrimSpace(sess.Name)
	if name == "" {
		return ""
	}
	return truncateRight(name, 40)
}
