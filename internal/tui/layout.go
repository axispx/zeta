package tui

import "github.com/axispx/zeta/internal/styles"

// Layout sizes every region from the terminal geometry: the transcript
// (viewport, wrap width, scrollbar column), the composer, and the in-flow gap.
// repaintTranscript is the paint entry point that keeps layout and the two
// caches in step.

// layout sizes chrome regions. m.transcript.showScrollbar reserves one column for the transcript scrollbar.
// Transcript height accounts for the in-flow gap (status/panel/idle) + input + footer.
// Filter overlays float over the transcript and do not consume layout rows.
func (m *Model) layout() {
	w := max(m.term.width, minTermW)

	inputH := max(m.composer.textarea.Height(), inputMinHeight)

	// gap + footer; input chrome is hidden while a panel replaces it.
	chromeH := m.gapHeight() + footerRows
	if !m.inputBlocked() {
		chromeH += inputH + styles.InputChromeV + styles.InputMarginB
	}
	th := max(m.term.height-chromeH, minTranscriptH)

	// Transcript region (pad + content + pad) may share the row with a scrollbar.
	// styles.Transcript pads ContentInset each side, so viewport width = contentW.
	regionW := w
	if m.transcript.showScrollbar {
		regionW -= scrollbarWidth
	}
	if regionW < minInputInnerW+2*styles.ContentInset {
		regionW = minInputInnerW + 2*styles.ContentInset
	}
	contentW := max(regionW-2*styles.ContentInset, minInputInnerW)

	// Input is inset by InputMarginH each side; scrollbar only affects transcript above.
	// lipgloss v2 Width includes padding → textarea = boxW - pad.
	inputInnerW := max(w-styles.InputChromeH-2*styles.InputMarginH, minInputInnerW)

	m.transcript.contentW = contentW
	m.transcript.viewport.SetWidth(contentW)
	m.transcript.viewport.SetHeight(th)
	m.composer.textarea.SetWidth(inputInnerW)
}

// layoutPreservingBottom re-runs layout and keeps stick-to-bottom scroll when
// chrome height changes (busy gap, overlay) without rewriting transcript content.
func (m *Model) layoutPreservingBottom() {
	atBottom := m.transcript.viewport.AtBottom()
	m.layout()
	if atBottom {
		m.transcript.viewport.GotoBottom()
	}
	m.transcript.invalidateMainView()
}

// refreshTranscript paints immediately and cancels any pending throttled paint.
func (m *Model) refreshTranscript() {
	m.cancelStreamPaint()
	m.repaintTranscript()
}

// repaintTranscript lays out and paints without touching the paint throttle.
//
// Remember if we were at the bottom before painting. Toggling the scrollbar
// changes wrap width, which can make AtBottom lie mid-paint — so only flip the
// bar when needed, then scroll back down if we started at the bottom.
func (m *Model) repaintTranscript() {
	m.transcript.invalidateMainView()
	if len(m.transcript.messages) == 0 {
		m.transcript.showScrollbar = false
		m.layout()
		m.transcript.viewport.SetContent("")
		return
	}

	stickBottom := m.transcript.viewport.AtBottom()
	m.layout()
	m.setTranscriptContent()
	needBar := m.transcript.viewport.TotalLineCount() > m.transcript.viewport.Height()
	if needBar != m.transcript.showScrollbar {
		m.transcript.showScrollbar = needBar
		m.layout()
		m.setTranscriptContent()
	}
	if stickBottom {
		m.transcript.viewport.GotoBottom()
	}
}
