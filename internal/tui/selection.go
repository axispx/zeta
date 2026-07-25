package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"

	"github.com/axispx/zeta/internal/styles"
)

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
	if !m.ready || len(m.messages) == 0 {
		return selPos{}, false
	}
	vh := m.viewport.Height()
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

	regionW := m.width
	if m.showScrollbar {
		regionW -= scrollbarWidth
	}
	if regionW < 1 {
		return selPos{}, false
	}

	col := x - styles.ContentInset
	if !clamp {
		if m.showScrollbar && x >= regionW {
			return selPos{}, false
		}
		if col < 0 || col >= m.contentW {
			return selPos{}, false
		}
		return selPos{line: m.viewport.YOffset() + row, col: col}, true
	}

	if col < 0 {
		col = 0
	}
	if m.contentW > 0 && col >= m.contentW {
		col = m.contentW - 1
	}
	if m.showScrollbar && x >= regionW && m.contentW > 0 {
		col = m.contentW - 1
	}
	return selPos{line: m.viewport.YOffset() + row, col: col}, true
}

// outsideTerminal reports whether (x,y) is outside the program surface.
// Terminals usually clamp coords, so this is best-effort; blur is the main leave path.
func (m *Model) outsideTerminal(x, y int) bool {
	if m.width < 1 || m.height < 1 {
		return false
	}
	return x < 0 || y < 0 || x >= m.width || y >= m.height
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
			m.sel.clear()
			return nil, false
		}
		if p, ok := m.transcriptPos(msg.X, msg.Y, false); ok {
			m.sel.start(p)
			return nil, true
		}
		m.sel.clear()
		return nil, false

	case tea.MouseMotionMsg:
		if !m.sel.dragging {
			return nil, false
		}
		if m.outsideTerminal(msg.X, msg.Y) {
			return m.finishSelectionDrag(), true
		}
		if p, ok := m.transcriptPos(msg.X, msg.Y, true); ok {
			m.sel.dragTo(p)
		}
		return nil, true

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || !m.sel.dragging {
			return nil, false
		}
		// Release position is authoritative (motion can be sparse).
		if !m.outsideTerminal(msg.X, msg.Y) {
			if p, ok := m.transcriptPos(msg.X, msg.Y, true); ok {
				m.sel.dragTo(p)
			}
		}
		return m.finishSelectionDrag(), true

	case tea.MouseWheelMsg:
		// Cancel in-progress drag; still fall through so the viewport scrolls.
		if m.sel.has() {
			m.sel.clear()
		}
		return nil, false
	}
	return nil, false
}

// finishSelectionDrag ends an in-progress drag and copies when the range is real.
// Used for mouse-up and for leave/blur (mouse left the terminal).
func (m *Model) finishSelectionDrag() tea.Cmd {
	if !m.sel.dragging {
		return nil
	}
	if !m.sel.moved() {
		m.sel.clear()
		return nil
	}
	text := m.selectedText()
	m.sel.clear()
	if text == "" {
		return nil
	}
	return tea.Batch(copyToClipboard(text), m.flashCopied())
}

// copyFlashMsg clears the "Copied" gap flash when gen still matches.
type copyFlashMsg struct{ gen int }

func (m *Model) flashCopied() tea.Cmd {
	m.copyFlashGen++
	gen := m.copyFlashGen
	m.copyFlash = true
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
	if !m.sel.has() {
		return ""
	}
	start, end := m.sel.normalized()
	return extractSelection(wrapContentLines(m.viewport.GetContent(), m.contentW), start, end)
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
