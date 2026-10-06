package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/harness"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/tools"
)

// denyReasonPlaceholder stands in for the reason on the Deny row while it holds
// the keys and nothing has been typed yet.
const denyReasonPlaceholder = "Type a reason…"

type permOption struct {
	label  string
	tag    string // key hint appended to the label, e.g. "(esc)"
	decide permission.Decision
}

// permOptions maps an approval's choices to rows. The choice set is core policy
// (harness.Approval.Choices); labels are this client's. Row order is the choice
// order, so the digit shortcut for a decision is its position here.
func permOptions(tool string, a harness.Approval) []permOption {
	opts := make([]permOption, 0, len(a.Choices))
	for _, d := range a.Choices {
		opts = append(opts, permOption{label: permLabel(tool, d, a), tag: permTag(d), decide: d})
	}
	return opts
}

// permLabel names a decision. Rows read as answers to the question above them
// ("Yes, proceed"), and a row that remembers something spells out its scope, so
// nothing about a grant has to be inferred from the verb.
func permLabel(tool string, d permission.Decision, a harness.Approval) string {
	switch d {
	case permission.AllowAlways:
		if tool == tools.Read {
			return "Yes, and don't ask again for this file"
		}
		return "Yes, and don't ask again for commands that start with " + codePrefix(a.Call.Rule.CommandPrefix)
	case permission.AllowSession:
		if tool == tools.Read {
			return "Yes, and don't ask again for this directory in this session"
		}
		return "Yes, and don't ask again for this command in this session"
	case permission.Deny:
		return "No, and tell zeta what to do differently"
	default: // AllowOnce
		return "Yes, proceed"
	}
}

// permTag is the dim key hint appended to a row, or "". Only rows that really do
// have a key of their own get one: Esc answers the prompt, so it is named on
// Deny. The other rows deliberately show no letter, because a stray keystroke
// over an open prompt must never decide it (docs/permissions.md).
func permTag(d permission.Decision) string {
	if d == permission.Deny {
		return " (esc)"
	}
	return ""
}

// codePrefix renders a command prefix as `code`. The prompt row is the only
// place the rule the user is agreeing to appears, so it is never trimmed here:
// the row renders at the terminal width and clips to it, which keeps one
// width-aware limit instead of a second, arbitrary one.
func codePrefix(prefix string) string {
	return "`" + prefix + "`"
}

// permFooter is the key legend under the row list.
const permFooter = "Press enter to confirm or esc to cancel"

// permissionPrompt is the modal approval surface (replaces the input while open).
// It carries the payload it is asking about — the command or the path — so the
// decision is answerable from the panel alone; a diff still lives on the active
// transcript tool row (Message.Out). Decisions go through turnSession.reply
// (harness-owned).
type permissionPrompt struct {
	label string
	name  string
	path  string
	// command is the bash command under approval, shown as the payload line.
	command string
	// review is the auto review's one-line account of why this prompt is open
	// (below the bar, risky label, unavailable), shown under the payload. Empty
	// when no review ran.
	review string
	appr   harness.Approval // derived from the tool name, args, and workspace root
	opts   []permOption     // source of truth for the row list and the decision dispatched for a chosen index
	list   optionList
	// reason is the freeform deny text typed on the last row.
	reason string
	// typing is true while the freeform row owns key input.
	typing bool
}

func newPermissionPrompt(label, name, path string) *permissionPrompt {
	p := &permissionPrompt{label: label, name: name, path: path}
	p.setApproval(harness.ApprovalFor(policy.Policy{}, "", name, nil))
	return p
}

// setOptions records the current choices and mirrors them into the row list.
func (p *permissionPrompt) setOptions(opts []permOption) {
	p.opts = opts
	rows := make([]optionRow, len(opts))
	for i, o := range opts {
		rows[i] = optionRow{label: o.label, tag: o.tag}
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
// placeholder) with a caret while it owns the keys, and the plain deny label
// otherwise. Derived state — call it before rendering or hit-testing, the only
// readers.
func (p *permissionPrompt) syncReason() {
	i := p.reasonRow()
	if i < 0 || i >= p.list.n() || i >= len(p.opts) {
		return
	}
	r := &p.list.rows[i]
	r.labelCursor, r.label = p.typing, p.opts[i].label
	switch {
	case strings.TrimSpace(p.reason) != "":
		r.label = p.reason
	case p.typing:
		r.label = denyReasonPlaceholder
	}
}

// setApproval records the derived approval and mirrors its choices into the rows.
func (p *permissionPrompt) setApproval(a harness.Approval) {
	p.appr = a
	p.setOptions(permOptions(p.name, a))
}

// setArgs derives the approval view of the call — the rule a persist decision
// writes, whether it is rememberable, and whether the target escapes the
// workspace — and records the payload the panel quotes back. plan is the live
// policy: it decides which part of a chain a remembered rule would cover.
func (p *permissionPrompt) setArgs(plan policy.Policy, args json.RawMessage, root string) {
	p.command = tools.ArgCommand(args)
	p.setApproval(harness.ApprovalFor(plan, root, p.name, args))
}

// sendReply delivers the UI's decision to the loop. Non-blocking: on cancel the
// loop may already have taken ctx.Done() and left the buffer free or stale.
func (m *Model) sendReply(r harness.Reply) {
	if m.turn.current != nil && m.turn.current.reply != nil {
		select {
		case m.turn.current.reply <- r:
		default:
		}
	}
}

// decidePermission applies the user's choice to the session and answers the
// harness. The grant/persist/reply logic is harness.Session.DecidePermission; reason
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

func (m *Model) permissionTitleH() int {
	_, contentW := overlayWidths(m.term.width)
	ink := m.term.chrome.OverlayInk()
	return lipgloss.Height(m.renderPermissionTitle(contentW, ink))
}

func (m *Model) renderPermission(width int) string {
	p := m.panel.perm
	if p == nil {
		return ""
	}
	p.syncReason()
	_, contentW := overlayWidths(width)
	ink := m.term.chrome.OverlayInk()

	body := m.renderPermissionTitle(contentW, ink) + p.list.render(contentW, ink) +
		"\n\n" + padPanel(ink.Hint.Width(contentW-panelGutter).Render(permFooter), panelGutter)
	return renderPanelFrame(m.term.chrome, width, body)
}

// renderPermissionTitle is the question the rows answer, then the payload it is
// about — the command or the path. The payload is repeated here rather than left
// to the transcript row behind the panel: the prompt is the decision surface, so
// it has to be answerable on its own. A payload ends with a newline, which the
// row list's own leading newline turns into the blank separator between them.
func (m *Model) renderPermissionTitle(contentW int, ink styles.OverlayInk) string {
	inner := contentW - panelGutter
	if inner < 1 {
		inner = 1
	}
	p := m.panel.perm
	if p == nil {
		return ""
	}
	question, payload := permTitle(p, ink)
	body := padPanel(ink.Header.Width(inner).Render(question), panelGutter)
	if payload == "" {
		return body
	}
	body += "\n\n" + padPanel(ink.Gap.Width(inner).Render(payload), panelGutter)
	if p.review != "" {
		// Set apart from the command by a blank line and dimmed, not italic:
		// context for the decision, not a second thing to answer.
		body += "\n\n" + padPanel(ink.Hint.Italic(false).Width(inner).Render(p.review), panelGutter)
	}
	return body + "\n"
}

// permTitle splits the prompt heading into the question and the payload it is
// about. The payload is "" when there is nothing more specific to show than the
// question itself.
func permTitle(p *permissionPrompt, ink styles.OverlayInk) (question, payload string) {
	c, ok := permission.ClassOf(p.name)
	if !ok {
		if p.name == tools.Read {
			if p.appr.Env {
				return "Would you like to read the following file?", codePath(ink, p.path, p.appr.Call.Outside)
			}
			dir := p.appr.Call.Dir
			if dir == "" {
				dir = p.path
			}
			if dir == "" {
				return "Would you like to access an external directory?", ""
			}
			return "Would you like to access the following directory?", codePath(ink, dir, p.appr.Call.Outside)
		}
		title := strings.TrimSpace(p.label)
		if title == "" {
			title = "Allow " + p.name + "?"
		}
		return title, ""
	}
	switch c {
	case permission.ClassBash:
		return "Would you like to run the following command?", codeCommand(ink, p)
	case permission.ClassEdit:
		if p.name == tools.Write {
			return "Would you like to write the following file?", codePath(ink, p.path, p.appr.Call.Outside)
		}
		return "Would you like to make the following edit?", codePath(ink, p.path, p.appr.Call.Outside)
	}
	return "", ""
}

// codePath is the payload line for a file or directory, marked when it escapes
// the workspace. The path is ordinary row text — it is the thing being decided,
// so it is not dimmed — on the panel fill, so it reads as part of the panel
// rather than as a band across it.
func codePath(ink styles.OverlayInk, path string, outside bool) string {
	if path == "" {
		return ""
	}
	line := ink.Row.Render(path)
	if outside {
		line += ink.Warn.Render(" (outside workspace)")
	}
	return line
}

// codeCommand is the payload line for a shell command: the command as it will be
// run, with a dim `$` prompt marker. The marker is quieter than the command so
// the eye lands on what is being approved.
func codeCommand(ink styles.OverlayInk, p *permissionPrompt) string {
	cmd := p.command
	if cmd == "" {
		return ""
	}
	return ink.Hint.Render("$ ") + ink.Row.Render(cmd)
}
