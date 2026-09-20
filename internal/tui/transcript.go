package tui

import (
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"

	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/version"
)

// This file owns the transcript surface end to end: the rendered message list
// and the viewport that scrolls it, the memos that keep repaints cheap (tx,
// mainCache), the live reasoning tail, and the drag selection that copies text
// out of it.
//
// The surface renders from its own state — messages, viewport, and the two
// memos — plus two inputs it does not own: the terminal theme, and the
// in-flight turn, whose live tail (thinking text, streaming answer, open tool
// row) is re-rendered every frame. Those are threaded through as arguments
// rather than reached for on Model, so both caches stay private to this file.
// Model's setTranscriptContent / mainView wrap them with the theme and live turn.

// transcript is the scrollable conversation surface: the rendered message
// list, the viewport that scrolls it, and the memos that keep repaints cheap.
type transcript struct {
	messages      []Message
	viewport      viewport.Model
	contentW      int // wrap width for transcript lines (matches styles.Transcript inset).
	showScrollbar bool
	sessionDiff   lineStats       // memo of sessionDiff(messages); refreshSessionDiff only
	tx            transcriptCache // frozen settled transcript; tail re-renders only
	mainCache     *mainViewCache  // memo of mainView() for transcript + gap; invalidated on transcript change
	paint         streamPaint     // throttled live redraw; gen survives turn boundaries
}

// selection is app-level transcript drag selection plus its copy flash.
type selection struct {
	sel          transcriptSel // app-level transcript drag selection
	copyFlash    bool          // brief "Copied" in the gap after a successful copy
	copyFlashGen int           // invalidates stale flash timers
}

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

// invalidate drops the settled-prefix cache and the painted-frame memo. Call
// whenever something baked into already-rendered rows changes — the theme, the
// wrap width, or the messages themselves.
func (t *transcript) invalidate() {
	t.tx.invalidate()
	t.invalidateMainView()
}

// setContent paints the viewport: cached prefix + fresh tail + thinking.
// Stick-to-bottom only when already at the bottom so stream paints don't yank
// the user back down after they scroll up (pgup / mouse wheel).
func (t *transcript) setContent(chrome styles.Chrome, turn *turnSession) {
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
func (t *transcript) syncPrefix(chrome styles.Chrome, turn *turnSession) {
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
func (t *transcript) liveFrom(turn *turnSession) int {
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
func (t *transcript) renderMessages(start, end int, chrome styles.Chrome, turn *turnSession) string {
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
func (t *transcript) buildTranscriptFull(chrome styles.Chrome, turn *turnSession) string {
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
	m.transcript.setContent(m.term.chrome, m.turn.current)
}

// Live reasoning tail: the recent CoT, dim, shown above the answer.
const (
	maxThinkingLines = 4    // live reasoning visual lines shown in the transcript
	maxThinkingBytes = 2048 // cap stored reasoning so long CoT cannot grow unbounded
)

// appendThinking concatenates a reasoning delta and keeps only the recent tail.
func appendThinking(prev, delta string) string {
	s := prev + delta
	if len(s) <= maxThinkingBytes {
		return s
	}
	s = s[len(s)-maxThinkingBytes:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// renderThinkingTail wraps reasoning text and keeps only the latest dim lines.
func renderThinkingTail(text string, width int) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	wrap := lipgloss.NewStyle()
	if width > 0 {
		wrap = wrap.Width(width)
	}
	lines := strings.Split(wrap.Render(text), "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > maxThinkingLines {
		lines = lines[len(lines)-maxThinkingLines:]
	}
	return styles.ThinkingMsg.Render(strings.Join(lines, "\n"))
}

// writeThinkingTail appends the live reasoning tail into the transcript builder.
func writeThinkingTail(b *strings.Builder, text string, width int) {
	tail := renderThinkingTail(text, width)
	if tail == "" {
		return
	}
	if b.Len() == 0 {
		// Empty transcript: match message top margin.
		tail = lipgloss.NewStyle().MarginTop(1).Render(tail)
	} else {
		b.WriteString("\n\n")
	}
	b.WriteString(tail)
}

// mainViewCache holds the last painted transcript chrome so no-op frames
// (rejected edge wheels, spinner-only ticks with unchanged offset) skip SoftWrap.
type mainViewCache struct {
	text string
	key  mainViewKey
}

type mainViewKey struct {
	yOff, w, h               int
	bar                      bool
	empty                    bool
	sel                      bool
	aLine, aCol, hLine, hCol int
}

func (t *transcript) invalidateMainView() {
	if t.mainCache != nil {
		*t.mainCache = mainViewCache{}
	}
}

// mainViewKey is the tray of inputs the painted frame depends on. Selection
// lives in selection, so it is passed in rather than reached for.
func (t transcript) mainViewKey(sel transcriptSel) mainViewKey {
	k := mainViewKey{
		yOff:  t.viewport.YOffset(),
		w:     t.viewport.Width(),
		h:     t.viewport.Height(),
		bar:   t.showScrollbar,
		empty: len(t.messages) == 0,
	}
	if sel.has() {
		start, end := sel.normalized()
		k.sel = true
		k.aLine, k.aCol = start.line, start.col
		k.hLine, k.hCol = end.line, end.col
	}
	return k
}

// rejectEdgeScroll drops wheel events that cannot move the transcript.
// Trackpad momentum keeps emitting past the edge; without this each tick still
// runs Update→View (SoftWrap walk) and reverse scrolls feel stuck until the
// backlog drains.
func (t *transcript) rejectEdgeScroll(msg tea.MouseWheelMsg) bool {
	switch msg.Button {
	case tea.MouseWheelUp:
		return t.viewport.AtTop()
	case tea.MouseWheelDown:
		return t.viewport.AtBottom()
	default:
		return false
	}
}

// mainView paints the transcript region: the banner when there is nothing to
// show, otherwise the viewport with the drag selection highlighted and the
// scrollbar beside it.
func (t *transcript) mainView(sel transcriptSel) string {
	w := t.viewport.Width()
	h := t.viewport.Height()
	if w <= 0 || h <= 0 {
		return ""
	}

	key := t.mainViewKey(sel)
	if t.mainCache != nil && t.mainCache.text != "" && t.mainCache.key == key {
		return t.mainCache.text
	}

	var inner string
	if len(t.messages) == 0 {
		banner := styles.Banner.Render(strings.TrimSpace(styles.BannerArt))
		ver := styles.Placeholder.Render("v" + version.Version)
		hero := lipgloss.JoinVertical(lipgloss.Center, banner, "", ver)
		inner = lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, hero)
	} else {
		inner = t.viewport.View()
		if sel.has() {
			start, end := sel.normalized()
			inner = highlightSelection(inner, t.viewport.YOffset(), start, end)
		}
	}

	body := styles.Transcript.Render(inner)
	out := body
	if t.showScrollbar {
		bar := renderScrollbar(h, t.viewport.TotalLineCount(), t.viewport.YOffset())
		out = lipgloss.JoinHorizontal(lipgloss.Top, body, bar)
	}
	if t.mainCache != nil {
		*t.mainCache = mainViewCache{text: out, key: key}
	}
	return out
}

// mainView paints the transcript region with the current drag selection.
func (m Model) mainView() string {
	return m.transcript.mainView(m.selection.sel)
}

// Drag selection over the transcript body, and the text it copies.
const (
	// selDragThreshold: ignore tiny moves so a plain click doesn't copy.
	// Measured in cells (manhattan line+col distance).
	selDragThreshold = 1

	// copiedFlashFor is how long the gap shows "Copied".
	// Idle gap is already GapBeforeInput (1 row); flash fills that slot — no relayout.
	copiedFlashFor = 1200 * time.Millisecond
)

// selPos is a cell in transcript display-line space (not terminal coords).
// Line is an absolute display line (viewport YOffset + row). Col is a cell
// column within the content width (after ContentInset; scrollbar excluded).
type selPos struct {
	line int
	col  int
}

// transcriptSel is an in-progress drag over the transcript body.
// The scrollbar is never part of the range — it is a sibling column in mainView.
// Selection exists only while dragging; mouse-up copies and clears.
type transcriptSel struct {
	dragging bool
	anchor   selPos
	head     selPos
}

func (s transcriptSel) has() bool { return s.dragging }

func (s *transcriptSel) clear() {
	*s = transcriptSel{}
}

func (s *transcriptSel) start(p selPos) {
	*s = transcriptSel{dragging: true, anchor: p, head: p}
}

func (s *transcriptSel) dragTo(p selPos) {
	if s.dragging {
		s.head = p
	}
}

// moved reports whether head is far enough from anchor to count as a drag.
func (s transcriptSel) moved() bool {
	dl := s.anchor.line - s.head.line
	if dl < 0 {
		dl = -dl
	}
	dc := s.anchor.col - s.head.col
	if dc < 0 {
		dc = -dc
	}
	return dl+dc > selDragThreshold
}

// normalized returns start/end with start <= end in reading order.
func (s transcriptSel) normalized() (start, end selPos) {
	a, b := s.anchor, s.head
	if a.line < b.line || (a.line == b.line && a.col <= b.col) {
		return a, b
	}
	return b, a
}

// transcriptPos maps terminal (x,y) into display-line space.
// clamp=false: miss for scrollbar, pad, outside viewport rows, empty transcript.
// clamp=true: project onto the content grid (drag extend past edges / scrollbar).
func (m *Model) transcriptPos(x, y int, clamp bool) (selPos, bool) {
	if !m.term.ready || len(m.transcript.messages) == 0 {
		return selPos{}, false
	}
	vh := m.transcript.viewport.Height()
	if vh < 1 {
		if !clamp {
			return selPos{}, false
		}
		vh = 1
	}

	row := y
	if clamp {
		if row < 0 {
			row = 0
		}
		if row >= vh {
			row = vh - 1
		}
	} else if y < 0 || y >= vh {
		return selPos{}, false
	}

	regionW := m.term.width
	if m.transcript.showScrollbar {
		regionW -= scrollbarWidth
	}
	if regionW < 1 {
		return selPos{}, false
	}

	col := x - styles.ContentInset
	if !clamp {
		if m.transcript.showScrollbar && x >= regionW {
			return selPos{}, false
		}
		if col < 0 || col >= m.transcript.contentW {
			return selPos{}, false
		}
		return selPos{line: m.transcript.viewport.YOffset() + row, col: col}, true
	}

	if col < 0 {
		col = 0
	}
	if m.transcript.contentW > 0 && col >= m.transcript.contentW {
		col = m.transcript.contentW - 1
	}
	if m.transcript.showScrollbar && x >= regionW && m.transcript.contentW > 0 {
		col = m.transcript.contentW - 1
	}
	return selPos{line: m.transcript.viewport.YOffset() + row, col: col}, true
}

// outsideTerminal reports whether (x,y) is outside the program surface.
// Terminals usually clamp coords, so this is best-effort; blur is the main leave path.
func (m *Model) outsideTerminal(x, y int) bool {
	if m.term.width < 1 || m.term.height < 1 {
		return false
	}
	return x < 0 || y < 0 || x >= m.term.width || y >= m.term.height
}

// handleSelectionMouse routes transcript drag-select mouse events.
// handled means Update should return without falling through (wheel never handles).
func (m *Model) handleSelectionMouse(msg tea.Msg) (cmd tea.Cmd, handled bool) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return nil, false
		}
		if m.picker.active || m.config.active {
			m.selection.sel.clear()
			return nil, false
		}
		if p, ok := m.transcriptPos(msg.X, msg.Y, false); ok {
			m.selection.sel.start(p)
			return nil, true
		}
		m.selection.sel.clear()
		return nil, false

	case tea.MouseMotionMsg:
		if !m.selection.sel.dragging {
			return nil, false
		}
		if m.outsideTerminal(msg.X, msg.Y) {
			return m.finishSelectionDrag(), true
		}
		if p, ok := m.transcriptPos(msg.X, msg.Y, true); ok {
			m.selection.sel.dragTo(p)
		}
		return nil, true

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || !m.selection.sel.dragging {
			return nil, false
		}
		// Release position is authoritative (motion can be sparse).
		if !m.outsideTerminal(msg.X, msg.Y) {
			if p, ok := m.transcriptPos(msg.X, msg.Y, true); ok {
				m.selection.sel.dragTo(p)
			}
		}
		return m.finishSelectionDrag(), true

	case tea.MouseWheelMsg:
		// Cancel in-progress drag; still fall through so the viewport scrolls.
		if m.selection.sel.has() {
			m.selection.sel.clear()
		}
		return nil, false
	}
	return nil, false
}

// finishSelectionDrag ends an in-progress drag and copies when the range is real.
// Used for mouse-up and for leave/blur (mouse left the terminal).
func (m *Model) finishSelectionDrag() tea.Cmd {
	if !m.selection.sel.dragging {
		return nil
	}
	if !m.selection.sel.moved() {
		m.selection.sel.clear()
		return nil
	}
	text := m.selectedText()
	m.selection.sel.clear()
	if text == "" {
		return nil
	}
	return tea.Batch(copyToClipboard(text), m.flashCopied())
}

// copyFlashMsg clears the "Copied" gap flash when gen still matches.
type copyFlashMsg struct{ gen int }

func (m *Model) flashCopied() tea.Cmd {
	m.selection.copyFlashGen++
	gen := m.selection.copyFlashGen
	m.selection.copyFlash = true
	return tea.Tick(copiedFlashFor, func(time.Time) tea.Msg {
		return copyFlashMsg{gen: gen}
	})
}

func copyToClipboard(text string) tea.Cmd {
	// Prefer native clipboard (pbcopy/wl-copy/xclip via atotto) — works locally
	// even when OSC 52 is blocked. OSC 52 covers SSH.
	s := text
	return tea.Sequence(
		func() tea.Msg {
			_ = clipboard.WriteAll(s)
			return nil
		},
		tea.SetClipboard(s),
	)
}

// selectedText extracts plain text for the current range from soft-wrapped
// GetContent lines (same wrap rules as viewport SoftWrap).
func (m *Model) selectedText() string {
	if !m.selection.sel.has() {
		return ""
	}
	start, end := m.selection.sel.normalized()
	return extractSelection(wrapContentLines(m.transcript.viewport.GetContent(), m.transcript.contentW), start, end)
}

// wrapContentLines splits content on \n then hard-wraps each line to width cells.
// Matches charmbracelet viewport SoftWrap (ansi.Cut chunks of width).
func wrapContentLines(content string, width int) []string {
	if content == "" {
		return nil
	}
	raw := strings.Split(content, "\n")
	if width < 1 {
		return raw
	}
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		out = append(out, wrapLine(line, width)...)
	}
	return out
}

func wrapLine(line string, width int) []string {
	if width < 1 {
		return []string{line}
	}
	lineW := ansi.StringWidth(line)
	if lineW <= width {
		return []string{line}
	}
	var parts []string
	for col := 0; col < lineW; col += width {
		parts = append(parts, ansi.Cut(line, col, col+width))
	}
	if len(parts) == 0 {
		return []string{""}
	}
	return parts
}

// selCols returns the exclusive [from,to) cell range for line li within [start,end].
func selCols(lineW, li int, start, end selPos) (from, to int, ok bool) {
	from, to = 0, lineW
	if li == start.line {
		from = start.col
	}
	if li == end.line {
		to = end.col + 1 // inclusive end cell → exclusive bound
	}
	if from < 0 {
		from = 0
	}
	if to > lineW {
		to = lineW
	}
	if from >= to {
		return 0, 0, false
	}
	return from, to, true
}

// extractSelection returns plain text for the inclusive cell range [start, end]
// over display lines. Scrollbar glyphs are never present in these lines.
func extractSelection(lines []string, start, end selPos) string {
	if len(lines) == 0 {
		return ""
	}
	if start.line < 0 {
		start.line = 0
	}
	if end.line >= len(lines) {
		end.line = len(lines) - 1
	}
	if start.line > end.line || start.line >= len(lines) {
		return ""
	}

	var b strings.Builder
	for li := start.line; li <= end.line; li++ {
		if li > start.line {
			b.WriteByte('\n')
		}
		plain := ansi.Strip(lines[li])
		from, to, ok := selCols(ansi.StringWidth(plain), li, start, end)
		if !ok {
			// Empty segment (blank line in a multi-line select still keeps the newline).
			continue
		}
		b.WriteString(strings.TrimRight(ansi.Cut(plain, from, to), " "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// extractSelectionString is a test helper over a newline-joined string.
func extractSelectionString(content string, start, end selPos) string {
	return extractSelection(strings.Split(content, "\n"), start, end)
}

// highlightSelection paints the active range onto a viewport-sized frame.
// frame is the unpadded viewport.View() string; yOffset is the scroll line.
func highlightSelection(frame string, yOffset int, start, end selPos) string {
	if frame == "" {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for i := range lines {
		li := yOffset + i
		if li < start.line || li > end.line {
			continue
		}
		from, to, ok := selCols(ansi.StringWidth(lines[i]), li, start, end)
		if !ok {
			continue
		}
		lines[i] = lipgloss.StyleRanges(lines[i], lipgloss.NewRange(from, to, styles.Selection))
	}
	return strings.Join(lines, "\n")
}
