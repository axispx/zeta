package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/styles"
)

// optionRow is one selectable row in a bottom-panel or overlay list.
type optionRow struct {
	key   string // optional hotkey (e.g. "a"); empty → numbered
	label string
	hint  string // right-side hint (description / mark)
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
func (o *optionList) handleClick(x, y, viewportH, termW, titleH int) (idx int, chose bool) {
	i := optionIndexAt(x, y, viewportH, termW, titleH, o.n())
	if i < 0 {
		return -1, false
	}
	o.selected = i
	return i, true
}

// handleMotion highlights the row under the cursor.
func (o *optionList) handleMotion(x, y, viewportH, termW, titleH int) bool {
	i := optionIndexAt(x, y, viewportH, termW, titleH, o.n())
	if i < 0 {
		return false
	}
	o.selected = i
	return true
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

// renderOptionRows paints a vertical list of accent rows (leading newline per row).
func renderOptionRows(rows []optionRow, selected, contentW int, ink styles.OverlayInk) string {
	if len(rows) == 0 {
		return ""
	}
	sel := clampOption(selected, len(rows))
	var b strings.Builder
	for i, r := range rows {
		b.WriteByte('\n')
		label := r.label
		if r.key != "" {
			label = "[" + r.key + "] " + r.label
		}
		hint := r.hint
		if hint != "" {
			hint = truncateRight(hint, max(8, contentW/3))
		}
		b.WriteString(formatAccentRow(label, hint, contentW, i == sel, false, ink))
	}
	return b.String()
}

// numberedRows builds rows as "1. label" with optional hints (no hotkey field).
func numberedRows(labels, hints []string) []optionRow {
	rows := make([]optionRow, len(labels))
	for i, lab := range labels {
		rows[i] = optionRow{label: fmt.Sprintf("%d. %s", i+1, lab)}
		if i < len(hints) {
			rows[i].hint = hints[i]
		}
	}
	return rows
}
