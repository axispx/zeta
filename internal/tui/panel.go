package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

// panelGutter aligns panel body text with the input prompt column.
const panelGutter = inputPromptWidth

// panel is the exclusive occupant of the input row: at most one of the
// permission / ask / plan / build widgets is open at a time. Open through set*
// and close through clear — never assign a field ad-hoc.
type panel struct {
	perm  *permissionPrompt
	ask   *askPrompt
	plan  *planPrompt
	build *buildPickPrompt
}

func (p *panel) blocked() bool {
	return p.perm != nil || p.ask != nil || p.plan != nil || p.build != nil
}

func (p *panel) clear() {
	p.perm = nil
	p.ask = nil
	p.plan = nil
	p.build = nil
}

func (p *panel) setPerm(perm *permissionPrompt) {
	p.clear()
	p.perm = perm
}

func (p *panel) setAsk(ask *askPrompt) {
	p.clear()
	p.ask = ask
}

func (p *panel) setPlan(plan *planPrompt) {
	p.clear()
	p.plan = plan
}

func (p *panel) setBuild(build *buildPickPrompt) {
	p.clear()
	p.build = build
}

// inputBlocked reports whether a panel owns the input slot.
func (m Model) inputBlocked() bool {
	return m.panel.blocked()
}

// clearPanel drops any panel without side effects (no deny reply).
func (m *Model) clearPanel() {
	m.panel.clear()
}

// abandonPanel cancels open harness panels so the agent unblocks (deny),
// then clears the slot. Used when the turn ends. Plan/build are post-turn.
func (m *Model) abandonPanel() {
	if m.panel.perm != nil {
		m.abandonPermission()
	}
	if m.panel.ask != nil {
		m.abandonAsk()
	}
}

// interruptPanel handles esc/ctrl+c for post-turn panels.
// Returns true when something was dismissed (caller should not quit further).
// Open harness panels (perm/ask) return false so finishTurn abandons them.
func (m *Model) interruptPanel() bool {
	switch {
	case m.panel.build != nil:
		m.cancelPlanBuildPick()
		return true
	case m.panel.plan != nil:
		m.dismissPlan()
		return true
	case m.panel.ask != nil, m.panel.perm != nil:
		return false
	default:
		return false
	}
}

// handlePanelKey routes keys to the exclusive panel.
// handled=false for esc (and when no panel) so Update's interrupt path runs.
func (m *Model) handlePanelKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case m.panel.perm != nil:
		return m.handlePermissionKey(msg)
	case m.panel.ask != nil:
		return m.handleAskKey(msg)
	case m.panel.plan != nil:
		return m.handlePlanKey(msg)
	case m.panel.build != nil:
		return m.handleBuildPickKey(msg)
	default:
		return nil, false
	}
}

// handlePanelClick routes mouse to the active panel.
func (m *Model) handlePanelClick(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	switch {
	case m.panel.perm != nil:
		return m.handlePermissionClick(msg)
	case m.panel.ask != nil:
		return m.handleAskClick(msg)
	case m.panel.plan != nil:
		return m.handlePlanClick(msg)
	case m.panel.build != nil:
		return m.handleBuildPickClick(msg)
	default:
		return nil, false
	}
}

func (m *Model) handlePanelMotion(msg tea.MouseMotionMsg) bool {
	switch {
	case m.panel.perm != nil:
		return m.handlePermissionMotion(msg)
	case m.panel.ask != nil:
		return m.handleAskMotion(msg)
	case m.panel.plan != nil:
		return m.handlePlanMotion(msg)
	case m.panel.build != nil:
		return m.handleBuildPickMotion(msg)
	default:
		return false
	}
}

// renderPanel returns the exclusive panel view, or "".
func (m Model) renderPanel(width int) string {
	switch {
	case m.panel.perm != nil:
		return m.renderPermission(width)
	case m.panel.ask != nil:
		return m.renderAsk(width)
	case m.panel.plan != nil:
		return m.renderPlanApproval(width)
	case m.panel.build != nil:
		return m.renderPlanBuildPick(width)
	default:
		return ""
	}
}

// renderPanelFrame wraps option-list body in the shared panel chrome
// (blank spacer + margin + overlay panel padding). Used by permission/ask/plan.
func renderPanelFrame(chrome styles.Chrome, width int, body string) string {
	innerW, _ := overlayWidths(width)
	panel := lipgloss.NewStyle().
		Margin(0, styles.InputMarginH, styles.InputMarginB, styles.InputMarginH).
		Render(chrome.OverlayPanel().
			Padding(1, styles.OverlayPadRight, 1, 0).
			Width(innerW).
			Render(body))
	return lipgloss.JoinVertical(lipgloss.Left, "", panel)
}

// padPanel indents multi-line body text so it aligns with the prompt column.
func padPanel(s string, pad int) string {
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

// optionIndexAt maps terminal (x,y) to a 0-based option row under a title block.
// titleH is the height of the panel header above the first option.
// Assumes one line per row (permission / plan lists); ask rows carry
// descriptions and use optionList.rowAt instead.
// Returns -1 when the point is outside the option list.
func optionIndexAt(x, y, viewportH, termW, titleH, nOpts int) int {
	if nOpts < 1 {
		return -1
	}
	rel := optionLineAt(x, y, viewportH, termW)
	if rel < 0 {
		return -1
	}
	idx := rel - titleH
	if idx < 0 || idx >= nOpts {
		return -1
	}
	return idx
}

// optionLineAt maps terminal (x,y) to a 0-based line below a title block,
// or -1 when the point is outside the option column.
func optionLineAt(x, y, viewportH, termW int) int {
	if termW < 1 {
		return -1
	}
	if x < styles.InputMarginH || x >= termW-styles.InputMarginH {
		return -1
	}
	// blank spacer + OverlayPanel top pad; gap starts right after the transcript.
	return y - viewportH - 1 - 1
}

// afterPanelChange re-lays out when a panel opens/closes while the TUI is ready.
func (m *Model) afterPanelChange() {
	if m.term.ready {
		m.layoutPreservingBottom()
	}
}
