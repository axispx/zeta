package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

// Busy-status labels shown above the input while a turn is in flight.
const (
	statusWaiting   = "Waiting"
	statusWorking   = "Working"
	statusReading   = "Reading"
	statusEditing   = "Editing"
	statusRunning   = "Running"
	statusSearching = "Searching"
	statusFetching  = "Fetching"
)

// turnStatusLine is the busy indicator above the input while a turn runs.
// Occupies the GapBeforeInput row so layout height stays stable.
func (m Model) turnStatusLine() string {
	label := m.busyLabel()
	if label == "" {
		return ""
	}
	// Keep the same left inset as the input box / footer.
	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH).
		Render(styles.SystemMsg.Render(m.spinner.View() + " " + label))
}

// busyLabel derives the chrome status from turn phase (no stored status field).
func (m Model) busyLabel() string {
	if m.turn == nil {
		return ""
	}
	if i := m.turn.activeTool; i >= 0 && i < len(m.messages) {
		return toolStatus(m.messages[i].Tool)
	}
	if m.turn.streaming {
		return statusWorking
	}
	return statusWaiting
}
