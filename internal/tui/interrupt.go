package tui

// turnCancelledText is appended to the transcript when the user aborts a turn.
const turnCancelledText = "Cancelled"

// tryInterrupt cancels the topmost interruptible UI/work state.
// Order: config → picker → bottom panel → overlays → compact → turn.
// Returns true when something was interrupted (Ctrl+C should not quit yet).
func (m *Model) tryInterrupt() bool {
	switch {
	case m.config.active:
		m.config.clear()
		return true
	case m.picker.active:
		m.picker.clear()
		return true
	case m.interruptBottom():
		return true
	case m.overlay.showing():
		m.dismissOverlay()
		return true
	case m.compacting:
		m.cancelCompact()
		return true
	case m.turn != nil:
		m.finishTurn()
		m.noteSystem(turnCancelledText)
		return true
	}
	return false
}
