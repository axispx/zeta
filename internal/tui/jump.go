package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/axispx/zeta/internal/styles"
)

// The "scroll to bottom" pill floats over the transcript's last row, right
// aligned just above the input, while the user is scrolled up. It takes no
// layout rows, so showing it never resizes the viewport.

const jumpLabel = " ↓ Scroll to bottom "

func (m *Model) jumpVisible() bool {
	return len(m.transcript.messages) > 0 &&
		!m.transcript.viewport.AtBottom() &&
		!m.filterOverlayOpen()
}

// jumpBounds returns the pill's cell range. It sits centered on the idle gap
// row, flush against the input; when the gap holds status or a panel it falls
// back to the transcript's last row.
func (m *Model) jumpBounds() (row, x0, x1 int) {
	w := lipgloss.Width(jumpLabel)
	row = m.transcript.viewport.Height() - 1
	if m.gapContent() == "" {
		row++
	}
	x0 = (m.term.width - w) / 2
	return row, x0, x0 + w
}

// withJumpButton paints the pill over the row jumpBounds names.
func (m *Model) withJumpButton(surface string) string {
	if !m.jumpVisible() {
		return surface
	}
	row, x0, x1 := m.jumpBounds()
	lines := strings.Split(surface, "\n")
	if row < 0 || row >= len(lines) || x0 < 0 {
		return surface
	}
	pill := styles.SystemMsg.Reverse(true).Render(jumpLabel)
	line := lines[row]
	if pad := x1 - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	lines[row] = ansi.Truncate(line, x0, "") + pill + ansi.TruncateLeft(line, x1, "")
	return strings.Join(lines, "\n")
}

// handleJumpClick scrolls to the bottom when the pill is clicked.
func (m *Model) handleJumpClick(msg tea.MouseClickMsg) bool {
	if msg.Button != tea.MouseLeft || !m.jumpVisible() {
		return false
	}
	row, x0, x1 := m.jumpBounds()
	if msg.Y != row || msg.X < x0 || msg.X >= x1 {
		return false
	}
	m.selection.sel.clear()
	m.transcript.viewport.GotoBottom()
	m.transcript.invalidateMainView()
	return true
}
