package tui

import tea "charm.land/bubbletea/v2"

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
	case m.overlay.mode != overlayOff:
		// Mode can stay overlayFiles while the list is hidden (no matches);
		// still dismiss so Esc does not fall through to cancel a turn.
		// Slash/model wipe their query; @ keeps the draft (see cancelOverlay).
		m.cancelOverlay()
		return true
	case m.compacting:
		m.cancelCompact()
		return true
	case m.authRetrying:
		// Recover cmd still completes; result handler installs creds, no restart.
		m.cancelAuthRetry()
		m.noteSystem(turnCancelledText)
		return true
	case m.turn != nil:
		// Late KindDone must not drain the queue.
		m.finishTurn()
		m.noteSystem(turnCancelledText)
		return true
	}
	return false
}

// handleCtrlC is the Ctrl+C ladder:
// edit/queue-focus → interrupt (config/picker/…/turn) → clear queue → quit.
func (m *Model) handleCtrlC() tea.Cmd {
	if m.handleQueueEsc() {
		return nil
	}
	if m.tryInterrupt() {
		return nil
	}
	if m.hasQueueState() {
		m.clearQueue()
		m.afterQueueChange()
		return nil
	}
	return m.requestQuit()
}
