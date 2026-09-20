package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/core"
	"github.com/axispx/zeta/internal/session"
)

type sessionTitleMsg struct {
	name string
	err  error
}

// requestSessionTitle builds the async name request. sess is a copy, which is
// fine: GenerateTitle does not mutate it, and ApplyTitle runs in the handler.
func requestSessionTitle(sess core.Session, client *ai.Client, prompt string) tea.Cmd {
	return func() tea.Msg {
		name, err := sess.GenerateTitle(context.Background(), client, prompt)
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

// ensureTitle requests an AI title once for an untitled session.
func (m *Model) ensureTitle(prompt string) tea.Cmd {
	if m.session.Client == nil || !m.session.WantsTitle() {
		return nil
	}
	m.session.TitlePending = true
	return requestSessionTitle(m.session, m.session.Client, prompt)
}

// firstUserPrompt is the oldest non-empty user text: the title seed for a
// resumed session, and the title prompt for a turn restarted by oauth retry.
func firstUserPrompt(msgs []Message) string {
	for _, msg := range msgs {
		if msg.Role == RoleUser {
			if t := strings.TrimSpace(msg.Text); t != "" {
				return t
			}
		}
	}
	return ""
}
