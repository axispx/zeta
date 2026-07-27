package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/version"
)

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

func (m *Model) invalidateMainView() {
	if m.mainCache != nil {
		*m.mainCache = mainViewCache{}
	}
}

func (m Model) mainViewKey() mainViewKey {
	k := mainViewKey{
		yOff:  m.viewport.YOffset(),
		w:     m.viewport.Width(),
		h:     m.viewport.Height(),
		bar:   m.showScrollbar,
		empty: len(m.messages) == 0,
	}
	if m.sel.has() {
		start, end := m.sel.normalized()
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
func (m *Model) rejectEdgeScroll(msg tea.MouseWheelMsg) bool {
	switch msg.Button {
	case tea.MouseWheelUp:
		return m.viewport.AtTop()
	case tea.MouseWheelDown:
		return m.viewport.AtBottom()
	default:
		return false
	}
}

func (m Model) mainView() string {
	w := m.viewport.Width()
	h := m.viewport.Height()
	if w <= 0 || h <= 0 {
		return ""
	}

	key := m.mainViewKey()
	if m.mainCache != nil && m.mainCache.text != "" && m.mainCache.key == key {
		return m.mainCache.text
	}

	var inner string
	if len(m.messages) == 0 {
		banner := styles.Banner.Render(strings.TrimSpace(styles.BannerArt))
		ver := styles.Placeholder.Render("v" + version.Version)
		hero := lipgloss.JoinVertical(lipgloss.Center, banner, "", ver)
		inner = lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, hero)
	} else {
		inner = m.viewport.View()
		if m.sel.has() {
			start, end := m.sel.normalized()
			inner = highlightSelection(inner, m.viewport.YOffset(), start, end)
		}
	}

	body := styles.Transcript.Render(inner)
	out := body
	if m.showScrollbar {
		bar := renderScrollbar(h, m.viewport.TotalLineCount(), m.viewport.YOffset())
		out = lipgloss.JoinHorizontal(lipgloss.Top, body, bar)
	}
	if m.mainCache != nil {
		*m.mainCache = mainViewCache{text: out, key: key}
	}
	return out
}
