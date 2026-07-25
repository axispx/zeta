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
	RoleTool
)

// ToolStatus is the lifecycle of a RoleTool row.
type ToolStatus int

const (
	ToolRunning ToolStatus = iota // call in flight (or awaiting permission)
	ToolOK                        // finished successfully
	ToolDenied                    // rejected by policy/user or cancelled
)

// Message is one turn in the chat transcript.
type Message struct {
	Role   Role
	Text   string
	Tool   string     // tool name for RoleTool
	Out    string     // live/final tool output (bash stdout / edit unified diff)
	Status ToolStatus // RoleTool lifecycle; zero value is ToolRunning

	// Cached markdown render for RoleAgent (keyed by source text + width).
	md       string
	mdWidth  int
	mdSource string
}

// newToolMessage builds a transcript tool row.
func newToolMessage(label, name string) Message {
	return Message{Role: RoleTool, Text: label, Tool: name}
}

// render formats the message for the transcript.
// When live is true, agent messages use progressive settled/tail rendering.
func (m *Message) render(width int, topMargin int, userMsg lipgloss.Style, live bool) string {
	body := m.renderBody(width, userMsg, live)
	if topMargin > 0 {
		return lipgloss.NewStyle().MarginTop(topMargin).Render(body)
	}
	return body
}

func (m *Message) renderBody(width int, userMsg lipgloss.Style, live bool) string {
	switch m.Role {
	case RoleUser:
		s := userMsg
		if width > 0 {
			s = s.Width(width)
		}
		return s.Render(m.Text)
	case RoleAgent:
		if live {
			return m.streamingMarkdown(width)
		}
		return m.cachedMarkdown(m.Text, width)
	case RoleTool:
		return widthBody(styles.ToolMsg.Render(m.Text), width)
	case RoleError:
		return widthBody(styles.ErrorMsg.Render(m.Text), width)
	default:
		return widthBody(styles.SystemMsg.Render(m.Text), width)
	}
}

// cachedMarkdown renders source via glamour, keyed by source+width.
func (m *Message) cachedMarkdown(source string, width int) string {
	if m.md != "" && m.mdWidth == width && m.mdSource == source {
		return m.md
	}
	out := renderMarkdown(source, width)
	m.md = out
	m.mdWidth = width
	m.mdSource = source
	return out
}

// streamingMarkdown styles settled blocks while the agent is still streaming,
// keeping the in-progress tail plain to avoid half-open fence/inline flicker.
// Reuses md cache keyed by the settled prefix so glamour runs only when a
// block boundary advances — not on every delta.
func (m *Message) streamingMarkdown(width int) string {
	settled, tail := streamSplit(m.Text)
	switch {
	case settled == "":
		return plainAgent(tail, width)
	case tail == "":
		return m.cachedMarkdown(m.Text, width)
	default:
		return m.cachedMarkdown(settled, width) + "\n\n" + plainAgent(tail, width)
	}
}

func widthBody(body string, width int) string {
	if width > 0 {
		return lipgloss.NewStyle().Width(width).Render(body)
	}
	return body
}
