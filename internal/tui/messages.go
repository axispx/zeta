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

	// Cached markdown render for RoleAgent (invalidated when Text/width change).
	md       string
	mdWidth  int
	mdSource string
}

// render formats the message for the transcript.
func (m *Message) render(width int, topMargin int) string {
	body := m.renderBody(width)
	if topMargin > 0 {
		return lipgloss.NewStyle().MarginTop(topMargin).Render(body)
	}
	return body
}

func (m *Message) renderBody(width int) string {
	switch m.Role {
	case RoleUser:
		s := styles.UserMsg
		if width > 0 {
			s = s.Width(width)
		}
		return s.Render(m.Text)
	case RoleAgent:
		return m.agentMarkdown(width)
	case RoleError:
		body := styles.ErrorMsg.Render(m.Text)
		if width > 0 {
			body = lipgloss.NewStyle().Width(width).Render(body)
		}
		return body
	default:
		body := styles.SystemMsg.Render(m.Text)
		if width > 0 {
			body = lipgloss.NewStyle().Width(width).Render(body)
		}
		return body
	}
}

func (m *Message) agentMarkdown(width int) string {
	if m.md != "" && m.mdWidth == width && m.mdSource == m.Text {
		return m.md
	}
	out := renderMarkdown(m.Text, width)
	m.md = out
	m.mdWidth = width
	m.mdSource = m.Text
	return out
}
