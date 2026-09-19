package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

// optionRow is one selectable row in a bottom-panel or overlay list.
type optionRow struct {
	key   string // optional hotkey (e.g. "a"); empty → numbered
	label string
	hint  string // description lines under the label
	// numbered rows get a "N. " prefix at render time, so a row's text can be
	// swapped without rebuilding the number (the freeform answer does this).
	numbered bool
	// labelCursor marks label as the live freeform answer: the text scrolls
	// from the left and carries a caret, like an input field.
	labelCursor bool
}

// optionList is the shared list controller for bottom-panel choice UIs
// (permission, plan approval, ask options). Owns selection + key/mouse.
type optionList struct {
	selected int
	rows     []optionRow
}

func (o *optionList) setRows(rows []optionRow) {
	o.rows = rows
	o.selected = clampOption(o.selected, len(rows))
}

func (o *optionList) n() int { return len(o.rows) }

// handleKey processes list navigation / hotkeys / enter.
// chose=true means the user confirmed selected (enter or hotkey).
// handled=false for esc so the interrupt path can run.
func (o *optionList) handleKey(msg tea.KeyPressMsg) (idx int, chose, handled bool) {
	n := o.n()
	if n < 1 {
		return 0, false, true
	}
	key := msg.String()
	switch key {
	case "esc":
		return 0, false, false
	case "up", "ctrl+p":
		o.selected = moveOption(o.selected, n, -1)
		return o.selected, false, true
	case "down", "ctrl+n":
		o.selected = moveOption(o.selected, n, 1)
		return o.selected, false, true
	case "enter":
		i := clampOption(o.selected, n)
		return i, true, true
	default:
		if i := keyOption(key, o.rows); i >= 0 {
			o.selected = i
			return i, true, true
		}
		if i := digitOption(key, n); i >= 0 {
			o.selected = i
			return i, false, true
		}
	}
	return o.selected, false, true // swallow other keys
}

// handleClick selects the row under (x,y). chose=true on left-click hit.
func (o *optionList) handleClick(x, y, viewportH, termW, titleH, contentW int) (idx int, chose bool) {
	i := o.rowAt(x, y, viewportH, termW, titleH, contentW)
	if i < 0 {
		return -1, false
	}
	o.selected = i
	return i, true
}

// handleMotion highlights the row under the cursor.
func (o *optionList) handleMotion(x, y, viewportH, termW, titleH, contentW int) bool {
	i := o.rowAt(x, y, viewportH, termW, titleH, contentW)
	if i < 0 {
		return false
	}
	o.selected = i
	return true
}

// rowAt maps terminal (x,y) to a row index, or -1. Rows carrying a hint span
// extra description lines, so the width used for wrapping is needed here too.
func (o *optionList) rowAt(x, y, viewportH, termW, titleH, contentW int) int {
	line := optionLineAt(x, y, viewportH, termW)
	if line < 0 {
		return -1
	}
	return o.rowAtLine(line-titleH, contentW)
}

// rowAtLine maps a 0-based line offset below the first row to a row index,
// counting each row's wrapped hint lines, or -1 past the last row.
func (o *optionList) rowAtLine(line, contentW int) int {
	if line < 0 {
		return -1
	}
	for i, r := range o.rows {
		h := 1 + len(optionHintLines(r, contentW))
		if line < h {
			return i
		}
		line -= h
	}
	return -1
}

func (o optionList) render(contentW int, ink styles.OverlayInk) string {
	return renderOptionRows(o.rows, o.selected, contentW, ink)
}

// moveOption steps selected within [0, n). Returns new index.
func moveOption(selected, n int, delta int) int {
	if n < 1 {
		return 0
	}
	selected += delta
	if selected < 0 {
		return 0
	}
	if selected >= n {
		return n - 1
	}
	return selected
}

// digitOption maps "1"…"9" to a 0-based index, or -1 if not a digit / out of range.
func digitOption(key string, n int) int {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return -1
	}
	idx := int(key[0] - '1')
	if idx < 0 || idx >= n {
		return -1
	}
	return idx
}

// keyOption maps a hotkey string to a row index, or -1.
func keyOption(key string, rows []optionRow) int {
	for i, r := range rows {
		if r.key != "" && key == r.key {
			return i
		}
	}
	return -1
}

// clampOption keeps selected in range (empty list → 0).
func clampOption(selected, n int) int {
	if n < 1 {
		return 0
	}
	if selected < 0 {
		return 0
	}
	if selected >= n {
		return n - 1
	}
	return selected
}

// optionHintIndent aligns a description under its row's label text
// ("→ " prompt + "1. " number prefix).
const optionHintIndent = inputPromptWidth + 3

// optionCaret marks the insertion point of a live freeform row.
const optionCaret = "█"

// renderOptionRows paints a vertical list of accent rows (leading newline per row).
// A row hint renders as dim, wrapped description lines under its label — a
// right-aligned column would squeeze descriptions into an unreadable sliver.
// A row whose label is live input (labelCursor) scrolls from the left and ends
// in a caret, so the newest keystrokes stay visible however long the answer gets.
func renderOptionRows(rows []optionRow, selected, contentW int, ink styles.OverlayInk) string {
	if len(rows) == 0 {
		return ""
	}
	sel := clampOption(selected, len(rows))
	indent := strings.Repeat(" ", optionHintIndent)
	var b strings.Builder
	for i, r := range rows {
		b.WriteByte('\n')
		prefix := ""
		switch {
		case r.key != "":
			prefix = "[" + r.key + "] "
		case r.numbered:
			prefix = fmt.Sprintf("%d. ", i+1)
		}
		label := prefix + r.label
		if r.labelCursor {
			// Keep the row number put and scroll the answer under it, reserving
			// the caret's column so the row never overflows.
			room := contentW - inputPromptWidth - lipgloss.Width(prefix) - 1
			label = prefix + truncateLeft(r.label, room) + optionCaret
		}
		b.WriteString(formatAccentRow(label, "", contentW, i == sel, false, ink))
		for _, line := range optionHintLines(r, contentW) {
			b.WriteByte('\n')
			b.WriteString(ink.Hint.Width(contentW).Render(indent + line))
		}
	}
	return b.String()
}

// optionHintLines wraps a row's hint into the unindented lines painted under its
// label. Nil when no hint. Indentation is the renderer's business, so wrapped
// widths stay stable.
func optionHintLines(r optionRow, contentW int) []string {
	hint := strings.TrimSpace(r.hint)
	if hint == "" {
		return nil
	}
	body := contentW - optionHintIndent
	if body < 8 {
		return []string{hint}
	}
	return strings.Split(wrapSimple(hint, body), "\n")
}

// numberedRows builds rows numbered "1."… at render time with optional
// descriptions (no hotkey field).
func numberedRows(labels, hints []string) []optionRow {
	rows := make([]optionRow, len(labels))
	for i, lab := range labels {
		rows[i] = optionRow{numbered: true, label: lab}
		if i < len(hints) {
			rows[i].hint = hints[i]
		}
	}
	return rows
}
