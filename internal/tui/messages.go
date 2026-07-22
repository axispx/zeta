package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

// Role identifies who produced a transcript message.
type Role int

const (
	RoleSystem Role = iota
	RoleUser
	RoleAgent
	RoleError
)

// Message is one turn in the chat transcript.
type Message struct {
	Role Role
	Text string
}

func (m Message) render(width int, topMargin int) string {
	var body string
	switch m.Role {
	case RoleUser:
		s := styles.UserMsg
		if width > 0 {
			s = s.Width(width)
		}
		body = s.Render(m.Text)
	case RoleAgent:
		s := styles.AgentMsg
		if width > 0 {
			s = s.Width(width)
		}
		body = s.Render(m.Text)
	case RoleError:
		body = styles.ErrorMsg.Render(m.Text)
		if width > 0 {
			body = lipgloss.NewStyle().Width(width).Render(body)
		}
	default:
		body = styles.SystemMsg.Render(m.Text)
		if width > 0 {
			body = lipgloss.NewStyle().Width(width).Render(body)
		}
	}
	if topMargin > 0 {
		return lipgloss.NewStyle().MarginTop(topMargin).Render(body)
	}
	return body
}
