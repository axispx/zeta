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

// denyReasonPlaceholder stands in for the reason on the Deny row while it holds
// the keys and nothing has been typed yet.
const denyReasonPlaceholder = "Type a reason…"

type permOption struct {
	label  string
	decide permission.Decision
}

// permOptions maps an approval's choices to rows. The choice set is core policy
// (core.Approval.Choices); labels are this client's. Row order is the choice
// order, so the digit shortcut for a decision is its position here.
func permOptions(tool string, a core.Approval) []permOption {
	opts := make([]permOption, 0, len(a.Choices))
	for _, d := range a.Choices {
		opts = append(opts, permOption{label: permLabel(tool, d, a), decide: d})
	}
	return opts
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
	// reason is the freeform deny text typed on the last row.
	reason string
	// typing is true while the freeform row owns key input.
	typing bool
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
		rows[i] = optionRow{label: o.label}
	}
	p.list.setRows(rows)
}

// reasonRow is the index of the Deny row — the row that doubles as a reason
// field once the user types on it — or -1 when the prompt offers no deny.
func (p *permissionPrompt) reasonRow() int {
	if p == nil {
		return -1
	}
	for i, o := range p.opts {
		if o.decide == permission.Deny {
			return i
		}
	}
	return -1
}

// denySelected reports whether the cursor sits on the Deny row, which is what
// lets typing start a reason instead of being swallowed.
func (p *permissionPrompt) denySelected() bool {
	return p.list.selected == p.reasonRow() && p.reasonRow() >= 0
}

// syncReason paints the Deny row as the reason field: the typed reason (or a
// placeholder) with a caret while it owns the keys, and the plain "Deny" label
// otherwise. Derived state — call it before rendering or hit-testing, the only
// readers.
func (p *permissionPrompt) syncReason() {
	i := p.reasonRow()
	if i < 0 || i >= p.list.n() || i >= len(p.opts) {
		return
	}
	r := &p.list.rows[i]
	r.hint, r.labelCursor, r.label = "", p.typing, p.opts[i].label
	switch {
	case strings.TrimSpace(p.reason) != "":
		r.label = p.reason
	case p.typing:
		r.label = denyReasonPlaceholder
	}
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
// agent. The grant/persist/reply logic is core.Session.DecidePermission; reason
// is an optional deny explanation the model sees on the rejected tool result.
func (m *Model) decidePermission(d permission.Decision, reason string) {
	p := m.panel.perm
	if p == nil {
		return
	}
	reply, err := m.session.DecidePermission(d, p.appr.Call, reason)
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
	m.decidePermission(permission.Deny, "")
}

// submitReason denies with the typed reason (a plain deny when empty).
func (m *Model) submitReason() {
	p := m.panel.perm
	if p == nil {
		return
	}
	m.decidePermission(permission.Deny, strings.TrimSpace(p.reason))
}

// handlePermissionKey consumes nav / row numbers / enter / esc while the prompt
// is open. A stray letter is swallowed, so typing never decides for you: ↑/↓ or
// a row number moves, Enter confirms. With Deny selected, typing starts a reason
// field on that row instead of being swallowed — the deny still needs Enter, so
// typing alone never answers the prompt.
//
// Esc denies: the prompt is a yes/no question, so the key that cancels elsewhere
// answers "no" here and the turn keeps going. Ctrl+C still aborts the turn
// (handleCtrlC runs before panel routing).
func (m *Model) handlePermissionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.panel.perm
	if p == nil {
		return nil, false
	}
	key := msg.String()
	// Esc answers the prompt, taking whatever reason is already typed. A live
	// transcript selection takes Esc first (deselect), so the highlight left by
	// copying a diff is not mistaken for a denial.
	if key == "esc" {
		if m.selection.sel.has() {
			return nil, false
		}
		m.submitReason()
		return nil, true
	}
	// An ↑/↓ press hands the keys back to the list so it still moves while a
	// reason is being typed instead of the arrow being swallowed.
	if p.typing && isAskMoveKey(key) {
		p.typing = false
	}
	if p.typing {
		return nil, m.handleReasonType(msg)
	}
	// Typing on the Deny row is the reason for the denial, not a stray key.
	// List moves are not text: they fall through so the cursor still moves.
	if p.denySelected() && !isAskMoveKey(key) {
		if t := askText(msg); t != "" {
			p.typing = true
			p.syncReason()
			return nil, m.handleReasonType(msg)
		}
	}
	idx, chose, handled := p.list.handleKey(msg)
	if !handled {
		return nil, false
	}
	if chose && idx >= 0 && idx < len(p.opts) {
		m.decidePermission(p.opts[idx].decide, "")
	}
	return nil, true
}

// handleReasonType edits the deny reason while the field owns the keys.
// Enter submits the deny; backspace at the empty field drops back to the list.
func (m *Model) handleReasonType(msg tea.KeyPressMsg) bool {
	p := m.panel.perm
	if p == nil {
		return false
	}
	if i := p.reasonRow(); i >= 0 {
		p.list.selected = i
	}
	cur := p.reason
	switch msg.String() {
	case "enter":
		m.submitReason()
	case "backspace", "ctrl+h":
		if cur != "" {
			r := []rune(cur)
			p.reason = string(r[:len(r)-1])
		} else {
			p.typing = false
		}
	case "ctrl+u":
		p.reason = ""
	case "ctrl+w":
		p.reason = trimLastWord(cur)
	default:
		if t := askText(msg); t != "" {
			p.reason = cur + t
		}
	}
	p.syncReason()
	return true
}

// handlePermissionClick selects an option under the cursor on left-click.
func (m *Model) handlePermissionClick(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	p := m.panel.perm
	if p == nil || msg.Button != tea.MouseLeft {
		return nil, false
	}
	p.syncReason()
	titleH := m.permissionTitleH()
	_, contentW := overlayWidths(m.term.width)
	idx, chose := p.list.handleClick(msg.X, msg.Y, m.transcript.viewport.Height(), m.term.width, titleH, contentW)
	if !chose {
		return nil, false
	}
	if idx >= 0 && idx < len(p.opts) {
		m.decidePermission(p.opts[idx].decide, "")
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
	p.syncReason()
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
	p.syncReason()
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
