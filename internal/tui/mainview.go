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

func (t *transcriptState) invalidateMainView() {
	if t.mainCache != nil {
		*t.mainCache = mainViewCache{}
	}
}

// mainViewKey is the tray of inputs the painted frame depends on. Selection
// lives in selectionState, so it is passed in rather than reached for.
func (t transcriptState) mainViewKey(sel transcriptSel) mainViewKey {
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
func (t *transcriptState) rejectEdgeScroll(msg tea.MouseWheelMsg) bool {
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
func (t *transcriptState) mainView(sel transcriptSel) string {
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
	return m.transcriptState.mainView(m.sel)
}
