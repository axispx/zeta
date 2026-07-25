package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

// panelGutter aligns bottom-panel body text with the input prompt column.
const panelGutter = inputPromptWidth

// bottomSlot is the exclusive input-slot panel (permission / ask / plan / build pick).
// At most one field is non-nil — always use set* / clear, never assign ad-hoc.
type bottomSlot struct {
	perm  *permissionPrompt
	ask   *askPrompt
	plan  *planPrompt
	build *buildPickPrompt
}

func (b *bottomSlot) blocked() bool {
	return b.perm != nil || b.ask != nil || b.plan != nil || b.build != nil
}

func (b *bottomSlot) clear() {
	b.perm = nil
	b.ask = nil
	b.plan = nil
	b.build = nil
}

func (b *bottomSlot) setPerm(p *permissionPrompt) {
	b.clear()
	b.perm = p
}

func (b *bottomSlot) setAsk(a *askPrompt) {
	b.clear()
	b.ask = a
}

func (b *bottomSlot) setPlan(p *planPrompt) {
	b.clear()
	b.plan = p
}

func (b *bottomSlot) setBuild(p *buildPickPrompt) {
	b.clear()
	b.build = p
}

// inputBlocked reports whether a bottom panel owns the input slot.
func (m Model) inputBlocked() bool {
	return m.bottom.blocked()
}

// clearBottom drops any bottom panel without side effects (no deny reply).
func (m *Model) clearBottom() {
	m.bottom.clear()
}

// abandonBottom cancels open harness panels so the agent unblocks (deny),
// then clears the slot. Used when the turn ends. Plan/build are post-turn.
func (m *Model) abandonBottom() {
	if m.bottom.perm != nil {
		m.abandonPermission()
	}
	if m.bottom.ask != nil {
		m.abandonAsk()
	}
}

// interruptBottom handles esc/ctrl+c for post-turn bottom panels.
// Returns true when something was dismissed (caller should not quit further).
// Open harness panels (perm/ask) return false so finishTurn abandons them.
func (m *Model) interruptBottom() bool {
	switch {
	case m.bottom.build != nil:
		m.cancelPlanBuildPick()
		return true
	case m.bottom.plan != nil:
		m.dismissPlan()
		return true
	case m.bottom.ask != nil, m.bottom.perm != nil:
		return false
	default:
		return false
	}
}

// handleBottomKey routes keys to the exclusive bottom panel.
// handled=false for esc (and when no panel) so Update's interrupt path runs.
func (m *Model) handleBottomKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case m.bottom.perm != nil:
		return m.handlePermissionKey(msg)
	case m.bottom.ask != nil:
		return m.handleAskKey(msg)
	case m.bottom.plan != nil:
		return m.handlePlanKey(msg)
	case m.bottom.build != nil:
		return m.handleBuildPickKey(msg)
	default:
		return nil, false
	}
}

// handleBottomClick routes mouse to the active panel.
func (m *Model) handleBottomClick(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	switch {
	case m.bottom.perm != nil:
		return m.handlePermissionClick(msg)
	case m.bottom.ask != nil:
		return m.handleAskClick(msg)
	case m.bottom.plan != nil:
		return m.handlePlanClick(msg)
	case m.bottom.build != nil:
		return m.handleBuildPickClick(msg)
	default:
		return nil, false
	}
}

func (m *Model) handleBottomMotion(msg tea.MouseMotionMsg) bool {
	switch {
	case m.bottom.perm != nil:
		return m.handlePermissionMotion(msg)
	case m.bottom.ask != nil:
		return m.handleAskMotion(msg)
	case m.bottom.plan != nil:
		return m.handlePlanMotion(msg)
	case m.bottom.build != nil:
		return m.handleBuildPickMotion(msg)
	default:
		return false
	}
}

// renderBottom returns the exclusive bottom-panel view, or "".
func (m Model) renderBottom(width int) string {
	switch {
	case m.bottom.perm != nil:
		return m.renderPermission(width)
	case m.bottom.ask != nil:
		return m.renderAsk(width)
	case m.bottom.plan != nil:
		return m.renderPlanApproval(width)
	case m.bottom.build != nil:
		return m.renderPlanBuildPick(width)
	default:
		return ""
	}
}

// renderBottomPanel wraps option-list body in the shared bottom-slot chrome
// (blank spacer + margin + overlay panel padding). Used by permission/ask/plan.
func renderBottomPanel(chrome styles.Chrome, width int, body string) string {
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
// Returns -1 when the point is outside the option list.
func optionIndexAt(x, y, viewportH, termW, titleH, nOpts int) int {
	if nOpts < 1 || termW < 1 {
		return -1
	}
	if x < styles.InputMarginH || x >= termW-styles.InputMarginH {
		return -1
	}
	// blank spacer + OverlayPanel top pad; gap starts right after the transcript.
	rel := y - viewportH - 1 - 1
	idx := rel - titleH
	if idx < 0 || idx >= nOpts {
		return -1
	}
	return idx
}

// afterSetBottom re-lays out when a panel opens/closes while the TUI is ready.
func (m *Model) afterSetBottom() {
	if m.ready {
		m.layoutPreservingBottom()
	}
}
