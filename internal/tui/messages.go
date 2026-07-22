package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

const maxGroupTools = 3 // visible tool lines before "+N more"

// Role identifies who produced a transcript message.
type Role int

const (
	RoleSystem Role = iota
	RoleUser
	RoleAgent
	RoleError
	RoleTool
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
func (m *Message) render(width int, topMargin int, userMsg lipgloss.Style) string {
	body := m.renderBody(width, userMsg)
	if topMargin > 0 {
		return lipgloss.NewStyle().MarginTop(topMargin).Render(body)
	}
	return body
}

func (m *Message) renderBody(width int, userMsg lipgloss.Style) string {
	switch m.Role {
	case RoleUser:
		s := userMsg
		if width > 0 {
			s = s.Width(width)
		}
		return s.Render(m.Text)
	case RoleAgent:
		return m.agentMarkdown(width)
	case RoleTool:
		return widthBody(styles.ToolMsg.Render(m.Text), width)
	case RoleError:
		return widthBody(styles.ErrorMsg.Render(m.Text), width)
	default:
		return widthBody(styles.SystemMsg.Render(m.Text), width)
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

// toolRunAt returns consecutive tool messages starting at i, or nil.
func toolRunAt(msgs []Message, i int) []Message {
	if i >= len(msgs) || msgs[i].Role != RoleTool {
		return nil
	}
	end := i + 1
	for end < len(msgs) && msgs[end].Role == RoleTool {
		end++
	}
	return msgs[i:end]
}

// renderToolGroup collapses consecutive tool messages into a compact block.
func renderToolGroup(msgs []Message, width, topMargin int) string {
	n := len(msgs)
	show := n
	if show > maxGroupTools {
		show = maxGroupTools
	}
	var b strings.Builder
	for i := 0; i < show; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(styles.ToolMsg.Render(msgs[i].Text))
	}
	if n > maxGroupTools {
		b.WriteByte('\n')
		b.WriteString(styles.SystemMsg.Render("+" + strconv.Itoa(n-maxGroupTools) + " more"))
	}
	body := widthBody(b.String(), width)
	if topMargin > 0 {
		return lipgloss.NewStyle().MarginTop(topMargin).Render(body)
	}
	return body
}

func widthBody(body string, width int) string {
	if width > 0 {
		return lipgloss.NewStyle().Width(width).Render(body)
	}
	return body
}
