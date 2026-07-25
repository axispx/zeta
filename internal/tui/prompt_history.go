package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/image"
)

// composerDraft is the live input state (text + image slots). Used to stash
// the draft while browsing session prompt history with ↑/↓.
type composerDraft struct {
	Text   string
	Images map[int]image.Ref
	NextN  int
}

// promptHistory recalls prior user turns into the input.
// Walks the UI transcript (m.messages), not the API history — compaction
// rewrites API history with checkpoints and drops old turns, which must not
// pollute up/down recall.
// at == -1 is the live draft; otherwise at indexes messages at a RoleUser row.
type promptHistory struct {
	at    int
	draft composerDraft // live stash while browsing
}

func (h *promptHistory) reset() {
	h.at = -1
	h.draft = composerDraft{}
}

func (h *promptHistory) live() bool { return h.at < 0 }

// stepUserMessage walks msgs from after/before from looking for RoleUser.
// dir is -1 (older) or +1 (newer). Returns -1 if none.
func stepUserMessage(msgs []Message, from, dir int) int {
	for i := from + dir; i >= 0 && i < len(msgs); i += dir {
		if msgs[i].Role == RoleUser {
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
//
// Recalled turns use display text only (image labels). Live draft image slots
// are stashed/restored so ↑ then ↓ does not drop in-progress attachments.
func (m *Model) handlePromptHistoryKey(msg tea.KeyPressMsg) bool {
	key := msg.String()
	older := key == "up" || key == "ctrl+p"
	newer := key == "down" || key == "ctrl+n"
	if !older && !newer {
		return false
	}

	h := &m.promptHist
	if h.at >= 0 && (h.at >= len(m.messages) || m.messages[h.at].Role != RoleUser) {
		h.reset()
	}

	if older {
		if m.textarea.Line() > 0 {
			return false
		}
		from := h.at
		if h.live() {
			from = len(m.messages)
		}
		next := stepUserMessage(m.messages, from, -1)
		if next < 0 {
			// Empty history while live: pass through. At oldest: consume.
			return !h.live()
		}
		if h.live() {
			h.draft = m.snapshotComposer()
		}
		h.at = next
		m.clearPendingImages()
		m.setPromptValue(m.messages[next].Text)
		return true
	}

	// newer
	if m.textarea.Line() < m.textarea.LineCount()-1 {
		return false
	}
	if h.live() {
		return false
	}
	next := stepUserMessage(m.messages, h.at, +1)
	if next < 0 {
		m.applyComposer(h.draft)
		h.reset()
		return true
	}
	h.at = next
	m.clearPendingImages()
	m.setPromptValue(m.messages[next].Text)
	return true
}
