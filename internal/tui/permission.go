package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/tools"
)

type permOption struct {
	key    string
	label  string
	decide permission.Decision
}

// permOptionsFor returns the approval choices for a tool.
// bash offers a class session grant; outside-workspace read offers a
// directory-scoped session grant. dotenv reads use the edit/write file prompt
// (persistable in-workspace). edit/write are allow-or-deny only, so every diff
// is reviewed and no rule can pre-approve a future mutation. When canPersist, an
// "always allow" row is added that writes a permission rule: a bash command
// prefix or an in-workspace dotenv file. prefix is the derived bash prefix,
// shown so the user sees the scope they are agreeing to. Hotkeys follow the
// usual approval shortcut (p = persist). Deny is per-call only; a persistent
// deny is a hand-edit of permissions.json.
func permOptionsFor(tool string, canPersist bool, prefix string, envFile bool) []permOption {
	if tool == tools.Read {
		if envFile {
			opts := []permOption{{"a", "Allow", permission.AllowOnce}}
			if canPersist {
				opts = append(opts, permOption{"p", "Always allow this file", permission.AllowAlways})
			}
			opts = append(opts, permOption{"d", "Deny", permission.Deny})
			return opts
		}
		return []permOption{
			{"a", "Allow once", permission.AllowOnce},
			{"s", "Allow this directory for session", permission.AllowSession},
			{"d", "Deny", permission.Deny},
		}
	}
	if permission.SessionGrantable(tool) {
		opts := []permOption{{"a", "Allow once", permission.AllowOnce}}
		if canPersist {
			opts = append(opts, permOption{"p", "Always allow " + codePrefix(prefix), permission.AllowAlways})
		}
		opts = append(opts, permOption{"s", "Allow for session", permission.AllowSession})
		opts = append(opts, permOption{"d", "Deny", permission.Deny})
		return opts
	}
	// edit/write: once-only, every diff reviewed.
	return []permOption{
		{"a", "Allow", permission.AllowOnce},
		{"d", "Deny", permission.Deny},
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
	label   string
	name    string
	path    string
	dir     string // outside-read session-grant directory
	env     bool   // dotenv secret; file prompt instead of directory grant
	outside bool   // path escapes the workspace root
	// rule + canPersist are the persisted rule an "always allow" decision writes.
	rule       policy.Rule
	canPersist bool
	// opts is the single source of truth for the rendered rows and the decision
	// dispatched for a chosen index.
	opts []permOption
	list optionList
}

func newPermissionPrompt(label, name, path string) *permissionPrompt {
	p := &permissionPrompt{label: label, name: name, path: path}
	p.setOptions(permOptionsFor(name, false, "", false))
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

// setArgs derives everything a persist decision needs from the raw call args, in
// one pass: the allow rule to write, whether it is rememberable, and whether the
// edit/write target escapes the workspace.
func (p *permissionPrompt) setArgs(args json.RawMessage, root string) {
	call := permission.CallFor(root, p.name, args)
	p.rule, p.canPersist, p.outside, p.dir = call.Rule, call.Persist, call.Outside, call.Dir
	p.env = permission.EnvFile(call.Match.Path)
	p.setOptions(permOptionsFor(p.name, p.canPersist, p.rule.CommandPrefix, p.env))
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
	p := m.bottom.perm
	if p == nil {
		return
	}
	switch d {
	case permission.AllowSession:
		if p.name == tools.Read {
			m.grants.GrantDir(p.dir)
		} else {
			m.grants.Grant(p.name)
		}
	case permission.AllowAlways:
		if p.canPersist {
			pol, err := policy.Add(p.rule)
			if err != nil {
				// Keep the decision even when the rule cannot be saved.
				m.noteError("permissions: " + err.Error())
			} else {
				// Replace in place so the agent's Gate sees the new rule too.
				m.rules.Replace(pol)
			}
		}
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
		if idx >= 0 && idx < len(p.opts) {
			m.decidePermission(p.opts[idx].decide)
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
	_, contentW := overlayWidths(m.width)
	idx, chose := p.list.handleClick(msg.X, msg.Y, m.viewport.Height(), m.width, titleH, contentW)
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
	p := m.bottom.perm
	if p == nil {
		return false
	}
	_, contentW := overlayWidths(m.width)
	return p.list.handleMotion(msg.X, msg.Y, m.viewport.Height(), m.width, m.permissionTitleH(), contentW)
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
		if p.name == tools.Read {
			if p.env {
				line := pathPermissionTitle(ink, "Read ", p.path, p.outside)
				return padPanel(ink.Gap.Width(inner).Render(line), panelGutter)
			}
			dir := p.dir
			if dir == "" {
				dir = p.path
			}
			var line string
			if dir == "" {
				line = ink.Header.Render("Access external directory")
			} else {
				line = pathPermissionTitle(ink, "Access ", dir, p.outside)
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
		line = pathPermissionTitle(ink, verb, p.path, p.outside)
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
