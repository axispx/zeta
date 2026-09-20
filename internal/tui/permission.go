package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/core"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/tools"
)

type permOption struct {
	key    string
	label  string
	decide permission.Decision
}

// permOptions maps an approval's choices to rows. The choice set is core policy
// (core.Approval.Choices); keys and labels are this client's.
func permOptions(tool string, a core.Approval) []permOption {
	opts := make([]permOption, 0, len(a.Choices))
	for _, d := range a.Choices {
		opts = append(opts, permOption{key: permKey(d), label: permLabel(tool, d, a), decide: d})
	}
	return opts
}

// permKey is the hotkey for a decision (p = persist), matching the usual
// approval shortcut. Deny is per-call only; a persistent deny is a hand-edit of
// permissions.json.
func permKey(d permission.Decision) string {
	switch d {
	case permission.AllowAlways:
		return "p"
	case permission.AllowSession:
		return "s"
	case permission.Deny:
		return "d"
	default:
		return "a"
	}
}

// permLabel names a decision. The "always allow" row shows the derived rule's
// scope so the user sees what they are agreeing to.
func permLabel(tool string, d permission.Decision, a core.Approval) string {
	switch d {
	case permission.AllowAlways:
		if tool == tools.Read {
			return "Always allow this file"
		}
		return "Always allow " + codePrefix(a.Call.Rule.CommandPrefix)
	case permission.AllowSession:
		if tool == tools.Read {
			return "Allow this directory for session"
		}
		return "Allow for session"
	case permission.Deny:
		return "Deny"
	default: // AllowOnce
		if permission.SessionGrantable(tool) || (tool == tools.Read && !a.Env) {
			return "Allow once"
		}
		return "Allow"
	}
}

// codePrefix renders a command prefix as `code`, trimming overly long commands.
func codePrefix(prefix string) string {
	return "`" + truncateRight(prefix, 24) + "`"
}

// permissionPrompt is the modal approval surface (replaces the input while open).
// Diff/command payloads live on the active transcript tool row (Message.Out /
// label), not in this panel. Decisions go through turnSession.reply (harness-owned).
type permissionPrompt struct {
	label string
	name  string
	path  string
	appr  core.Approval // derived from the tool name, args, and workspace root
	opts  []permOption  // source of truth for the row list and the decision dispatched for a chosen index
	list  optionList
}

func newPermissionPrompt(label, name, path string) *permissionPrompt {
	p := &permissionPrompt{label: label, name: name, path: path}
	p.setApproval(core.ApprovalFor("", name, nil))
	return p
}

// setOptions records the current choices and mirrors them into the row list.
func (p *permissionPrompt) setOptions(opts []permOption) {
	p.opts = opts
	rows := make([]optionRow, len(opts))
	for i, o := range opts {
		rows[i] = optionRow{key: o.key, label: o.label}
	}
	p.list.setRows(rows)
}

// setApproval records the derived approval and mirrors its choices into the rows.
func (p *permissionPrompt) setApproval(a core.Approval) {
	p.appr = a
	p.setOptions(permOptions(p.name, a))
}

// setArgs derives the approval view of the call — the rule a persist decision
// writes, whether it is rememberable, and whether the target escapes the
// workspace.
func (p *permissionPrompt) setArgs(args json.RawMessage, root string) {
	p.setApproval(core.ApprovalFor(root, p.name, args))
}

// sendReply delivers a harness decision to the agent. Non-blocking: on cancel the
// agent may already have taken ctx.Done() and left the buffer free or stale.
func (m *Model) sendReply(r agent.Reply) {
	if m.turn.current != nil && m.turn.current.reply != nil {
		select {
		case m.turn.current.reply <- r:
		default:
		}
	}
}

// decidePermission applies the user's choice to the session and answers the
// agent. The grant/persist/reply logic is core.Session.DecidePermission.
func (m *Model) decidePermission(d permission.Decision) {
	p := m.panel.perm
	if p == nil {
		return
	}
	reply, err := m.session.DecidePermission(d, p.appr.Call)
	if err != nil {
		// Keep the decision even when the rule cannot be saved.
		m.noteError("permissions: " + err.Error())
	}
	m.sendReply(reply)
	m.panel.clear()
	m.afterPanelChange()
}

// abandonPermission sends Deny so the agent unblocks on the same path as a
// user deny, then clears the prompt. Used when the turn is cancelled.
func (m *Model) abandonPermission() {
	if m.panel.perm == nil {
		return
	}
	m.decidePermission(permission.Deny)
}

// handlePermissionKey consumes nav / a/s/d / enter while the prompt is open.
// Esc returns handled=false so Update's interrupt path still runs.
func (m *Model) handlePermissionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.panel.perm
	if p == nil {
		return nil, false
	}
	idx, chose, handled := p.list.handleKey(msg)
	if !handled {
		return nil, false
	}
	if chose {
		if idx >= 0 && idx < len(p.opts) {
			m.decidePermission(p.opts[idx].decide)
		}
	}
	return nil, true
}

// handlePermissionClick selects an option under the cursor on left-click.
func (m *Model) handlePermissionClick(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	p := m.panel.perm
	if p == nil || msg.Button != tea.MouseLeft {
		return nil, false
	}
	titleH := m.permissionTitleH()
	_, contentW := overlayWidths(m.term.width)
	idx, chose := p.list.handleClick(msg.X, msg.Y, m.transcript.viewport.Height(), m.term.width, titleH, contentW)
	if !chose {
		return nil, false
	}
	if idx >= 0 && idx < len(p.opts) {
		m.decidePermission(p.opts[idx].decide)
		return nil, true
	}
	return nil, false
}

// handlePermissionMotion highlights the option under the cursor.
func (m *Model) handlePermissionMotion(msg tea.MouseMotionMsg) bool {
	p := m.panel.perm
	if p == nil {
		return false
	}
	_, contentW := overlayWidths(m.term.width)
	return p.list.handleMotion(msg.X, msg.Y, m.transcript.viewport.Height(), m.term.width, m.permissionTitleH(), contentW)
}

func (m Model) permissionTitleH() int {
	_, contentW := overlayWidths(m.term.width)
	ink := m.term.chrome.OverlayInk()
	return lipgloss.Height(m.renderPermissionTitle(contentW, ink))
}

// permissionOptionAt returns the option index at terminal (x,y), or -1.
// Used by tests.
func (m Model) permissionOptionAt(x, y int) int {
	if m.panel.perm == nil {
		return -1
	}
	return optionIndexAt(x, y, m.transcript.viewport.Height(), m.term.width, m.permissionTitleH(), m.panel.perm.list.n())
}

func (m Model) renderPermission(width int) string {
	p := m.panel.perm
	if p == nil {
		return ""
	}
	_, contentW := overlayWidths(width)
	ink := m.term.chrome.OverlayInk()

	body := m.renderPermissionTitle(contentW, ink) + p.list.render(contentW, ink)
	return renderPanelFrame(m.term.chrome, width, body)
}

func (m Model) renderPermissionTitle(contentW int, ink styles.OverlayInk) string {
	inner := contentW - panelGutter
	if inner < 1 {
		inner = 1
	}
	p := m.panel.perm
	if p == nil {
		return ""
	}
	c, ok := permission.ClassOf(p.name)
	if !ok {
		if p.name == tools.Read {
			if p.appr.Env {
				line := pathPermissionTitle(ink, "Read ", p.path, p.appr.Call.Outside)
				return padPanel(ink.Gap.Width(inner).Render(line), panelGutter)
			}
			dir := p.appr.Call.Dir
			if dir == "" {
				dir = p.path
			}
			var line string
			if dir == "" {
				line = ink.Header.Render("Access external directory")
			} else {
				line = pathPermissionTitle(ink, "Access ", dir, p.appr.Call.Outside)
			}
			return padPanel(ink.Gap.Width(inner).Render(line), panelGutter)
		}
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
		line = pathPermissionTitle(ink, verb, p.path, p.appr.Call.Outside)
	}
	return padPanel(ink.Gap.Width(inner).Render(line), panelGutter)
}

// pathPermissionTitle is "Edit path (outside workspace)" / "Access path" / etc.
func pathPermissionTitle(ink styles.OverlayInk, verb, path string, outside bool) string {
	if path == "" {
		return ink.Header.Render(strings.TrimSpace(verb) + " file")
	}
	line := ink.Header.Render(verb) + ink.Gap.Render(styles.DiffFile.Render(path))
	if outside {
		line += ink.Gap.Render(styles.OutsideWarn.Render(" (outside workspace)"))
	}
	return line
}
