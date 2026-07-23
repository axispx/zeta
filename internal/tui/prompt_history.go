package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
)

// promptHistory recalls prior user turns from session history into the input.
// at == -1 is the live draft; otherwise at indexes history at a RoleUser message.
type promptHistory struct {
	at    int
	draft string
}

func (h *promptHistory) reset() {
	h.at = -1
	h.draft = ""
}

func (h *promptHistory) live() bool { return h.at < 0 }

// stepUser walks history from after/before from looking for RoleUser.
// dir is -1 (older) or +1 (newer). Returns -1 if none.
func stepUser(history []ai.Message, from, dir int) int {
	for i := from + dir; i >= 0 && i < len(history); i += dir {
		if history[i].Role == ai.RoleUser {
			return i
		}
	}
	return -1
}

func (m *Model) resetPromptHistory() { m.promptHist.reset() }

func (m *Model) setPromptValue(s string) {
	prevH := m.textarea.Height()
	m.textarea.SetValue(s)
	m.syncTextareaStyles()
	if m.textarea.Height() != prevH {
		m.refreshTranscript()
	}
}

// notePromptEdit exits browse mode when the input diverges from the recalled turn.
func (m *Model) notePromptEdit(before string) {
	if !m.promptHist.live() && m.textarea.Value() != before {
		m.promptHist.reset()
	}
}

// handlePromptHistoryKey navigates session prompt history with up/down or ctrl+p/n.
// Returns true when the key was consumed (including no-op at history edges).
// Shell-style: only leaves the draft when the cursor is on the first/last line.
func (m *Model) handlePromptHistoryKey(msg tea.KeyPressMsg) bool {
	key := msg.String()
	older := key == "up" || key == "ctrl+p"
	newer := key == "down" || key == "ctrl+n"
	if !older && !newer {
		return false
	}

	h := &m.promptHist
	if h.at >= 0 && (h.at >= len(m.history) || m.history[h.at].Role != ai.RoleUser) {
		h.reset()
	}

	if older {
		if m.textarea.Line() > 0 {
			return false
		}
		from := h.at
		if h.live() {
			from = len(m.history)
		}
		next := stepUser(m.history, from, -1)
		if next < 0 {
			// Empty history while live: pass through. At oldest: consume.
			return !h.live()
		}
		if h.live() {
			h.draft = m.textarea.Value()
		}
		h.at = next
		m.setPromptValue(m.history[next].Text)
		return true
	}

	// newer
	if m.textarea.Line() < m.textarea.LineCount()-1 {
		return false
	}
	if h.live() {
		return false
	}
	next := stepUser(m.history, h.at, +1)
	if next < 0 {
		m.setPromptValue(h.draft)
		h.reset()
		return true
	}
	h.at = next
	m.setPromptValue(m.history[next].Text)
	return true
}
