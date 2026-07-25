package tui

import (
	"strings"
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

// setTranscriptContent paints the viewport: cached prefix + fresh tail + thinking.
// Stick-to-bottom only when already at the bottom so stream paints don't yank
// the user back down after they scroll up (pgup / mouse wheel).
func (m *Model) setTranscriptContent() {
	m.syncPrefix()
	atBottom := m.viewport.AtBottom()
	var b strings.Builder
	b.WriteString(joinBlocks(m.tx.prefix, m.renderMessages(m.tx.frozen, len(m.messages))))
	if m.turn != nil && m.turn.thinking != "" {
		writeThinkingTail(&b, m.turn.thinking, m.contentW)
	}
	m.viewport.SetContent(b.String())
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// syncPrefix makes tx.prefix == render(messages[0:liveFrom]).
func (m *Model) syncPrefix() {
	if m.contentW != m.tx.width || m.tx.frozen > len(m.messages) {
		m.tx = transcriptCache{width: m.contentW}
	}

	liveFrom := m.liveFrom()
	switch {
	case liveFrom == m.tx.frozen:
		// already in sync
	case liveFrom < m.tx.frozen:
		// e.g. a new tool joined a run we had already frozen — rebuild
		m.tx.prefix = m.renderMessages(0, liveFrom)
		m.tx.frozen = liveFrom
	default:
		// newly settled messages — append to prefix
		m.tx.prefix = joinBlocks(m.tx.prefix, m.renderMessages(m.tx.frozen, liveFrom))
		m.tx.frozen = liveFrom
	}
}

// liveFrom is the first message index that may still change this frame.
func (m *Model) liveFrom() int {
	n := len(m.messages)
	if n == 0 || m.turn == nil {
		return n
	}
	// Streaming tool output: whole consecutive tool block is live.
	if i := m.turn.activeTool; i >= 0 && i < n {
		return toolRunStart(m.messages, i)
	}
	// Streaming answer: last agent row is live.
	if m.turn.streaming && m.messages[n-1].Role == RoleAgent {
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
func (m *Model) renderMessages(start, end int) string {
	if start >= end {
		return ""
	}

	userMsg := m.chrome.UserMsg()
	streaming := m.turn != nil && m.turn.streaming
	atEnd := end == len(m.messages)

	var b strings.Builder
	for i := start; i < end; {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		top := 0
		if i == 0 {
			top = 1
		}

		if run := toolRunAt(m.messages, i); run != nil {
			b.WriteString(renderToolGroup(run, m.contentW, top))
			i += len(run)
			continue
		}

		msg := &m.messages[i]
		live := streaming && atEnd && i == len(m.messages)-1 && msg.Role == RoleAgent
		b.WriteString(msg.render(m.contentW, top, userMsg, live))
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
func (m *Model) buildTranscriptFull() string {
	var b strings.Builder
	b.WriteString(m.renderMessages(0, len(m.messages)))
	if m.turn != nil && m.turn.thinking != "" {
		writeThinkingTail(&b, m.turn.thinking, m.contentW)
	}
	return b.String()
}
