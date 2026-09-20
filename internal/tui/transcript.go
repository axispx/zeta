package tui

import (
	"strings"

	"github.com/axispx/zeta/internal/styles"
)

// transcriptCache stores the rendered text of settled messages so stream/tool
// updates only re-render the live tail.
//
//	messages:  [ 0 …… frozen )  [ frozen …… n )
//	             ↑ prefix         ↑ re-rendered each refresh
type transcriptCache struct {
	width  int
	prefix string
	frozen int // len(messages) covered by prefix
}

func (c *transcriptCache) invalidate() { *c = transcriptCache{} }

// The transcript surface renders from its own state — messages, viewport, and
// the two memos (tx, mainCache) — plus two inputs it does not own: the terminal
// theme, and the in-flight turn, whose live tail (thinking text, streaming
// answer, open tool row) is re-rendered every frame. Those are threaded through
// as arguments rather than reached for on Model, so both caches stay private to
// this file and mainview.go. Model's setTranscriptContent / mainView wrap them
// with the theme and live turn.

// invalidate drops the settled-prefix cache and the painted-frame memo. Call
// whenever something baked into already-rendered rows changes — the theme, the
// wrap width, or the messages themselves.
func (t *transcriptState) invalidate() {
	t.tx.invalidate()
	t.invalidateMainView()
}

// setContent paints the viewport: cached prefix + fresh tail + thinking.
// Stick-to-bottom only when already at the bottom so stream paints don't yank
// the user back down after they scroll up (pgup / mouse wheel).
func (t *transcriptState) setContent(chrome styles.Chrome, turn *turnSession) {
	t.syncPrefix(chrome, turn)
	atBottom := t.viewport.AtBottom()
	var b strings.Builder
	b.WriteString(joinBlocks(t.tx.prefix, t.renderMessages(t.tx.frozen, len(t.messages), chrome, turn)))
	if turn != nil && turn.thinking != "" {
		writeThinkingTail(&b, turn.thinking, t.contentW)
	}
	t.viewport.SetContent(b.String())
	t.invalidateMainView()
	if atBottom {
		t.viewport.GotoBottom()
	}
}

// syncPrefix makes tx.prefix == render(messages[0:liveFrom]).
func (t *transcriptState) syncPrefix(chrome styles.Chrome, turn *turnSession) {
	if t.contentW != t.tx.width || t.tx.frozen > len(t.messages) {
		t.tx = transcriptCache{width: t.contentW}
	}

	liveFrom := t.liveFrom(turn)
	switch {
	case liveFrom == t.tx.frozen:
		// already in sync
	case liveFrom < t.tx.frozen:
		// e.g. a new tool joined a run we had already frozen — rebuild
		t.tx.prefix = t.renderMessages(0, liveFrom, chrome, turn)
		t.tx.frozen = liveFrom
	default:
		// newly settled messages — append to prefix
		t.tx.prefix = joinBlocks(t.tx.prefix, t.renderMessages(t.tx.frozen, liveFrom, chrome, turn))
		t.tx.frozen = liveFrom
	}
}

// liveFrom is the first message index that may still change this frame.
func (t *transcriptState) liveFrom(turn *turnSession) int {
	n := len(t.messages)
	if n == 0 || turn == nil {
		return n
	}
	// Streaming tool output: whole consecutive tool block is live.
	if i := turn.activeTool; i >= 0 && i < n {
		return toolRunStart(t.messages, i)
	}
	// Streaming answer: last agent row is live.
	if turn.streaming && t.messages[n-1].Role == RoleAgent {
		return n - 1
	}
	// Otherwise (thinking, waiting) everything is settled.
	return n
}

// toolRunStart is the first index of the consecutive RoleTool run containing i.
func toolRunStart(msgs []Message, i int) int {
	for i > 0 && msgs[i-1].Role == RoleTool {
		i--
	}
	return i
}

// renderMessages renders messages[start:end). Top margin only on message 0.
// Callers pass liveFrom-aligned ranges (never mid tool-run).
func (t *transcriptState) renderMessages(start, end int, chrome styles.Chrome, turn *turnSession) string {
	if start >= end {
		return ""
	}

	userMsg := chrome.UserMsg()
	streaming := turn != nil && turn.streaming
	atEnd := end == len(t.messages)

	var b strings.Builder
	for i := start; i < end; {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		top := 0
		if i == 0 {
			top = 1
		}

		if run := toolRunAt(t.messages, i); run != nil {
			b.WriteString(renderToolGroup(run, t.contentW, top))
			i += len(run)
			continue
		}

		msg := &t.messages[i]
		live := streaming && atEnd && i == len(t.messages)-1 && msg.Role == RoleAgent
		b.WriteString(msg.render(t.contentW, top, userMsg, live))
		i++
	}
	return b.String()
}

// joinBlocks joins two transcript chunks with a blank line (empty parts dropped).
func joinBlocks(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "\n\n" + b
	}
}

// buildTranscriptFull renders everything with no cache (tests only).
func (t *transcriptState) buildTranscriptFull(chrome styles.Chrome, turn *turnSession) string {
	var b strings.Builder
	b.WriteString(t.renderMessages(0, len(t.messages), chrome, turn))
	if turn != nil && turn.thinking != "" {
		writeThinkingTail(&b, turn.thinking, t.contentW)
	}
	return b.String()
}

// setTranscriptContent paints the viewport from the transcript cache, the
// current theme, and the live turn — the two inputs the surface depends on but
// does not own.
func (m *Model) setTranscriptContent() {
	m.transcriptState.setContent(m.chrome, m.turn)
}
