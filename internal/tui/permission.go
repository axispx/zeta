package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/styles"
)

const (
	// permissionGutter matches inputPromptWidth so the title aligns with option
	// labels (the → occupies the gutter; body text starts in the same column).
	permissionGutter = inputPromptWidth
)

type permOption struct {
	key    string
	label  string
	decide permission.Decision
}

var permOptions = []permOption{
	{"a", "Allow once", permission.AllowOnce},
	{"s", "Allow for session", permission.AllowSession},
	{"d", "Deny", permission.Deny},
}

// permissionPrompt is the modal approval surface (replaces the input while open).
// Diff/command payloads live on the active transcript tool row (Message.Out /
// label), not in this panel. Decisions go through turnSession.reply (harness-owned).
type permissionPrompt struct {
	label    string
	name     string
	path     string
	selected int // index into permOptions
}

// sendDecision delivers allow/deny to the agent. Non-blocking: on cancel the
// agent may already have taken ctx.Done() and left the buffer free or stale.
func (m *Model) sendDecision(d permission.Decision) {
	if m.turn != nil && m.turn.reply != nil {
		select {
		case m.turn.reply <- d != permission.Deny:
		default:
		}
	}
}

func (m *Model) decidePermission(d permission.Decision) {
	if m.perm == nil {
		return
	}
	if d == permission.AllowSession {
		m.grants.Grant(m.perm.name)
	}
	m.sendDecision(d)
	m.perm = nil
	if m.ready {
		m.layoutPreservingBottom()
	}
}

// abandonPermission sends Deny so the agent unblocks on the same path as a
// user deny, then clears the prompt. Used when the turn is cancelled.
func (m *Model) abandonPermission() {
	if m.perm == nil {
		return
	}
	m.decidePermission(permission.Deny)
}

// handlePermissionKey consumes nav / a/s/d / enter while the prompt is open.
// Other keys are swallowed so the approval UI can't leak into overlays/input.
// Esc returns false so Update's interrupt path still runs.
func (m *Model) handlePermissionKey(msg tea.KeyPressMsg) bool {
	if m.perm == nil {
		return false
	}
	switch msg.String() {
	case "esc":
		return false
	case "up", "ctrl+p":
		if m.perm.selected > 0 {
			m.perm.selected--
		}
	case "down", "ctrl+n":
		if m.perm.selected < len(permOptions)-1 {
			m.perm.selected++
		}
	case "enter":
		i := m.perm.selected
		if i < 0 || i >= len(permOptions) {
			i = 0
		}
		m.decidePermission(permOptions[i].decide)
	default:
		for _, opt := range permOptions {
			if msg.String() == opt.key {
				m.decidePermission(opt.decide)
				return true
			}
		}
	}
	return true
}

// handlePermissionClick selects an option under the cursor on left-click.
func (m *Model) handlePermissionClick(msg tea.MouseClickMsg) bool {
	if m.perm == nil || msg.Button != tea.MouseLeft {
		return false
	}
	if i := m.permissionOptionAt(msg.X, msg.Y); i >= 0 {
		m.decidePermission(permOptions[i].decide)
		return true
	}
	return false
}

// handlePermissionMotion highlights the option under the cursor.
func (m *Model) handlePermissionMotion(msg tea.MouseMotionMsg) bool {
	if m.perm == nil {
		return false
	}
	if i := m.permissionOptionAt(msg.X, msg.Y); i >= 0 {
		m.perm.selected = i
		return true
	}
	return false
}

// permissionOptionAt returns the option index at terminal (x,y), or -1.
func (m Model) permissionOptionAt(x, y int) int {
	if m.perm == nil || m.width < 1 {
		return -1
	}
	if x < styles.InputMarginH || x >= m.width-styles.InputMarginH {
		return -1
	}
	_, contentW := overlayWidths(m.width)
	ink := m.chrome.OverlayInk()
	// blank spacer + OverlayPanel top pad; gap starts right after the transcript.
	rel := y - m.viewport.Height() - 1 - 1
	titleH := lipgloss.Height(m.renderPermissionTitle(contentW, ink))
	idx := rel - titleH
	if idx < 0 || idx >= len(permOptions) {
		return -1
	}
	return idx
}

func (m Model) renderPermission(width int) string {
	if m.perm == nil {
		return ""
	}
	innerW, contentW := overlayWidths(width)
	ink := m.chrome.OverlayInk()

	var b strings.Builder
	b.WriteString(m.renderPermissionTitle(contentW, ink))

	sel := m.perm.selected
	if sel < 0 || sel >= len(permOptions) {
		sel = 0
	}
	for i, opt := range permOptions {
		b.WriteByte('\n')
		label := "[" + opt.key + "] " + opt.label
		b.WriteString(formatAccentRow(label, "", contentW, i == sel, false, ink))
	}

	panel := lipgloss.NewStyle().
		Margin(0, styles.InputMarginH, styles.InputMarginB, styles.InputMarginH).
		Render(m.chrome.OverlayPanel().
			Padding(1, styles.OverlayPadRight, 1, 0).
			Width(innerW).
			Render(b.String()))
	// One blank row above the approval prompt (matches busy-status breathing room).
	return lipgloss.JoinVertical(lipgloss.Left, "", panel)
}

func (m Model) renderPermissionTitle(contentW int, ink styles.OverlayInk) string {
	inner := contentW - permissionGutter
	if inner < 1 {
		inner = 1
	}
	p := m.perm
	if p == nil {
		return ""
	}
	c, ok := permission.ClassOf(p.name)
	if !ok {
		title := strings.TrimSpace(p.label)
		if title == "" {
			title = "Allow " + p.name + "?"
		}
		return padPermission(ink.Header.Width(inner).Render(title), permissionGutter)
	}
	var line string
	switch c {
	case permission.ClassBash:
		line = ink.Header.Render("Run this ") +
			ink.Kbd.Render("bash") +
			ink.Header.Render(" command?")
	case permission.ClassEdit:
		verb := "Edit "
		if p.name == "write" {
			verb = "Write "
		}
		if p.path != "" {
			line = ink.Header.Render(verb) + ink.Gap.Render(styles.DiffFile.Render(p.path))
		} else {
			line = ink.Header.Render(strings.TrimSpace(verb) + " file")
		}
	}
	return padPermission(ink.Gap.Width(inner).Render(line), permissionGutter)
}

func padPermission(s string, pad int) string {
	if pad <= 0 || s == "" {
		return s
	}
	prefix := strings.Repeat(" ", pad)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
