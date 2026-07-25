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

// gapContent is the gap slot between transcript and input: bottom panel
// (permission / ask / plan), command/model overlay, busy status, copy flash, or empty.
// Priority: bottom panel → overlay → busy → copy flash.
func (m Model) gapContent() string {
	if p := m.renderBottom(m.width); p != "" {
		return p
	}
	if ov := m.renderOverlay(m.width); ov != "" {
		return ov
	}
	if busy := m.turnStatusLine(); busy != "" {
		return busy
	}
	if m.copyFlash {
		return copiedFlashLine()
	}
	return ""
}

// copiedFlashLine is a one-row "Copied" hint in the gap after a successful copy.
func copiedFlashLine() string {
	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH).
		Render(styles.SystemMsg.Render("Copied"))
}

// gapHeight is the layout rows for the gap slot between transcript and input.
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
