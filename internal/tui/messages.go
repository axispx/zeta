package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/plan"
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

	// framePlan: Plan-mode ingest snapshot. Framing does not follow later mode
	// switches. Raw Text still holds tags for API/JSONL; render splits only when true.
	framePlan bool

	// Cached markdown render for RoleAgent (keyed by source text + width).
	// For plan-framed rows the cache stores the full composed output under m.Text.
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
		return m.renderAgent(width, live)
	case RoleTool:
		return widthBody(styles.ToolMsg.Render(m.Text), width)
	case RoleError:
		return widthBody(styles.ErrorMsg.Render(m.Text), width)
	default:
		return widthBody(styles.SystemMsg.Render(m.Text), width)
	}
}

// renderAgent paints assistant text. framePlan gates plan framing; otherwise
// raw markdown (tags stay ordinary text).
//
// Settled renders cache the full composed string under m.Text so multi-segment
// plan rows (before / frame / after) do not thrash a single-segment cache.
func (m *Message) renderAgent(width int, live bool) string {
	if m.framePlan {
		if before, body, after, ok := plan.DisplayParts(m.Text); ok {
			if !live {
				if m.md != "" && m.mdWidth == width && m.mdSource == m.Text {
					return m.md
				}
				out := composeAgentPlan(before, body, after, width, false, nil)
				m.md = out
				m.mdWidth = width
				m.mdSource = m.Text
				return out
			}
			// Live: compose each paint; streamingMarkdown may use md for the after tail only.
			return composeAgentPlan(before, body, after, width, true, m)
		}
	}
	if live {
		return m.streamingMarkdown(m.Text, width)
	}
	return m.cachedMarkdown(m.Text, width)
}

// composeAgentPlan builds framed plan output. When live and msg is non-nil,
// after uses streamingMarkdown; otherwise segments call renderMarkdown directly
// (no multi-source cache thrash).
func composeAgentPlan(before, body, after string, width int, live bool, msg *Message) string {
	var parts []string
	if strings.TrimSpace(before) != "" {
		parts = append(parts, renderMarkdown(before, width))
	}
	if body != "" {
		open := live && msg != nil && plan.Open(msg.Text)
		parts = append(parts, renderPlanFrame(body, width, open))
	}
	if strings.TrimSpace(after) != "" {
		if live && msg != nil {
			parts = append(parts, msg.streamingMarkdown(after, width))
		} else {
			parts = append(parts, renderMarkdown(after, width))
		}
	}
	return strings.Join(parts, "\n\n")
}

// renderPlanFrame draws plan markdown with a yellow left border (no tags).
func renderPlanFrame(body string, width int, live bool) string {
	innerW := width
	// thick left border (1) + PaddingLeft(1)
	const frameChrome = 2
	if innerW > frameChrome {
		innerW -= frameChrome
	}
	var content string
	if live {
		content = plainAgent(body, innerW)
	} else {
		content = renderMarkdown(body, innerW)
	}
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return ""
	}
	frame := styles.PlanFrame
	if width > 0 {
		frame = frame.Width(width)
	}
	return frame.Render(content)
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
func (m *Message) streamingMarkdown(source string, width int) string {
	settled, tail := streamSplit(source)
	switch {
	case settled == "":
		return plainAgent(tail, width)
	case tail == "":
		return m.cachedMarkdown(source, width)
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
