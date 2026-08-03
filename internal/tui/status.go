package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

// Busy-status labels shown above the input while a turn is in flight.
const (
	statusWaiting   = "Waiting"
	statusThinking  = "Thinking"
	statusWorking   = "Working"
	statusReading   = "Reading"
	statusEditing   = "Editing"
	statusRunning   = "Running"
	statusSearching = "Searching"
	statusFetching  = "Fetching"

	// busyStatusRows is blank + spinner + blank so the label is not flush
	// against the transcript or input. layout() sizes the viewport for this.
	busyStatusRows = 3
)

// turnStatusLine is the busy indicator above the input while a turn runs.
// Height is always busyStatusRows when non-empty (see gapHeight).
func (m Model) turnStatusLine() string {
	label := m.busyLabel()
	if label == "" {
		return ""
	}
	status := lipgloss.NewStyle().
		Margin(0, styles.InputMarginH).
		Render(styles.SystemMsg.Render(m.spinner.View() + " " + label))
	return lipgloss.JoinVertical(lipgloss.Left, "", status, "")
}

// gapContent is the in-flow slot between transcript and input: bottom panel
// (permission / ask / plan), busy status, follow-ups, copy flash, or empty.
// Filter overlays are not in-flow — they float over the transcript bottom
// (pinOverlayBottom) so opening a picker does not resize the viewport.
//
// Priority: bottom panel → busy + follow-ups → copy flash → empty (idle blank).
func (m Model) gapContent() string {
	if p := m.renderBottom(m.width); p != "" {
		return p
	}
	busy := m.turnStatusLine()
	q := m.renderQueueFollowups(m.width)
	switch {
	case busy != "" && q != "":
		return lipgloss.JoinVertical(lipgloss.Left, busy, q)
	case busy != "":
		return busy
	case q != "":
		return q
	case m.copyFlash:
		return copiedFlashLine()
	default:
		return ""
	}
}

// copiedFlashLine is a one-row "Copied" hint in the gap after a successful copy.
func copiedFlashLine() string {
	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH).
		Render(styles.SystemMsg.Render("Copied"))
}

// filterOverlayOpen reports a slash/model/@ picker above the input.
// Bottom panels own the composer; no floating overlay while input is blocked.
func (m Model) filterOverlayOpen() bool {
	return !m.inputBlocked() && m.overlay.showing()
}

// gapHeight is the in-flow rows between transcript and input.
// Idle blank stays reserved while a filter overlay is open so opening a picker
// does not jump the transcript (list pins over the blank / status gap).
func (m Model) gapHeight() int {
	if g := m.gapContent(); g != "" {
		if h := lipgloss.Height(g); h > 0 {
			return h
		}
	}
	return styles.GapBeforeInput
}

// busyLabel derives the chrome status from turn phase (no stored status field).
func (m Model) busyLabel() string {
	if m.compacting {
		return statusCompacting
	}
	if m.turn == nil {
		return ""
	}
	if i := m.turn.activeTool; i >= 0 && i < len(m.messages) {
		return toolStatus(m.messages[i].Tool)
	}
	if m.turn.streaming {
		return statusWorking
	}
	if m.turn.thinking != "" {
		return statusThinking
	}
	return statusWaiting
}
