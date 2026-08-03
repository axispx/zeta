package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/tools"
)

type permOption struct {
	key    string
	label  string
	decide permission.Decision
}

// permOptionsFor returns the approval choices for a tool.
// bash offers session grant; edit/write stay once-only so every diff is reviewed.
func permOptionsFor(tool string) []permOption {
	if permission.SessionGrantable(tool) {
		return []permOption{
			{"a", "Allow once", permission.AllowOnce},
			{"s", "Allow for session", permission.AllowSession},
			{"d", "Deny", permission.Deny},
		}
	}
	return []permOption{
		{"a", "Allow", permission.AllowOnce},
		{"d", "Deny", permission.Deny},
	}
}

func permOptionRows(tool string) []optionRow {
	opts := permOptionsFor(tool)
	rows := make([]optionRow, len(opts))
	for i, o := range opts {
		rows[i] = optionRow{key: o.key, label: o.label}
	}
	return rows
}

// permissionPrompt is the modal approval surface (replaces the input while open).
// Diff/command payloads live on the active transcript tool row (Message.Out /
// label), not in this panel. Decisions go through turnSession.reply (harness-owned).
type permissionPrompt struct {
	label string
	name  string
	path  string
	list  optionList
}

func newPermissionPrompt(label, name, path string) *permissionPrompt {
	p := &permissionPrompt{label: label, name: name, path: path}
	p.list.setRows(permOptionRows(name))
	return p
}

// sendReply delivers a harness decision to the agent. Non-blocking: on cancel the
// agent may already have taken ctx.Done() and left the buffer free or stale.
func (m *Model) sendReply(r agent.Reply) {
	if m.turn != nil && m.turn.reply != nil {
		select {
		case m.turn.reply <- r:
		default:
		}
	}
}

func (m *Model) decidePermission(d permission.Decision) {
	if m.bottom.perm == nil {
		return
	}
	if d == permission.AllowSession {
		m.grants.Grant(m.bottom.perm.name)
	}
	if d == permission.Deny {
		m.sendReply(agent.DenyTool())
	} else {
		m.sendReply(agent.RunTool())
	}
	m.bottom.clear()
	m.afterSetBottom()
}

// abandonPermission sends Deny so the agent unblocks on the same path as a
// user deny, then clears the prompt. Used when the turn is cancelled.
func (m *Model) abandonPermission() {
	if m.bottom.perm == nil {
		return
	}
	m.decidePermission(permission.Deny)
}

// handlePermissionKey consumes nav / a/s/d / enter while the prompt is open.
// Esc returns handled=false so Update's interrupt path still runs.
func (m *Model) handlePermissionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.bottom.perm
	if p == nil {
		return nil, false
	}
	idx, chose, handled := p.list.handleKey(msg)
	if !handled {
		return nil, false
	}
	if chose {
		opts := permOptionsFor(p.name)
		if idx >= 0 && idx < len(opts) {
			m.decidePermission(opts[idx].decide)
		}
	}
	return nil, true
}

// handlePermissionClick selects an option under the cursor on left-click.
func (m *Model) handlePermissionClick(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	p := m.bottom.perm
	if p == nil || msg.Button != tea.MouseLeft {
		return nil, false
	}
	titleH := m.permissionTitleH()
	idx, chose := p.list.handleClick(msg.X, msg.Y, m.viewport.Height(), m.width, titleH)
	if !chose {
		return nil, false
	}
	opts := permOptionsFor(p.name)
	if idx >= 0 && idx < len(opts) {
		m.decidePermission(opts[idx].decide)
		return nil, true
	}
	return nil, false
}

// handlePermissionMotion highlights the option under the cursor.
func (m *Model) handlePermissionMotion(msg tea.MouseMotionMsg) bool {
	p := m.bottom.perm
	if p == nil {
		return false
	}
	return p.list.handleMotion(msg.X, msg.Y, m.viewport.Height(), m.width, m.permissionTitleH())
}

func (m Model) permissionTitleH() int {
	_, contentW := overlayWidths(m.width)
	ink := m.chrome.OverlayInk()
	return lipgloss.Height(m.renderPermissionTitle(contentW, ink))
}

// permissionOptionAt returns the option index at terminal (x,y), or -1.
// Used by tests.
func (m Model) permissionOptionAt(x, y int) int {
	if m.bottom.perm == nil {
		return -1
	}
	return optionIndexAt(x, y, m.viewport.Height(), m.width, m.permissionTitleH(), m.bottom.perm.list.n())
}

func (m Model) renderPermission(width int) string {
	p := m.bottom.perm
	if p == nil {
		return ""
	}
	_, contentW := overlayWidths(width)
	ink := m.chrome.OverlayInk()

	body := m.renderPermissionTitle(contentW, ink) + p.list.render(contentW, ink)
	return renderBottomPanel(m.chrome, width, body)
}

func (m Model) renderPermissionTitle(contentW int, ink styles.OverlayInk) string {
	inner := contentW - panelGutter
	if inner < 1 {
		inner = 1
	}
	p := m.bottom.perm
	if p == nil {
		return ""
	}
	c, ok := permission.ClassOf(p.name)
	if !ok {
		title := strings.TrimSpace(p.label)
		if title == "" {
			title = "Allow " + p.name + "?"
		}
		return padPanel(ink.Header.Width(inner).Render(title), panelGutter)
	}
	var line string
	switch c {
	case permission.ClassBash:
		line = ink.Header.Render("Run this ") +
			ink.Kbd.Render(tools.Bash) +
			ink.Header.Render(" command?")
	case permission.ClassEdit:
		verb := "Edit "
		if p.name == tools.Write {
			verb = "Write "
		}
		if p.path != "" {
			line = ink.Header.Render(verb) + ink.Gap.Render(styles.DiffFile.Render(p.path))
		} else {
			line = ink.Header.Render(strings.TrimSpace(verb) + " file")
		}
	}
	return padPanel(ink.Gap.Width(inner).Render(line), panelGutter)
}
