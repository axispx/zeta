package tui

import "github.com/axispx/zeta/internal/styles"

// Layout sizes every region from the terminal geometry: the transcript
// (viewport, wrap width), the composer, and the in-flow gap.
// repaintTranscript is the paint entry point that keeps layout and the two
// caches in step.

// layout sizes chrome regions.
// Transcript height accounts for the in-flow gap (status/panel/idle) + input + footer.
// Filter overlays float over the transcript and do not consume layout rows.
func (m *Model) layout() {
	w := max(m.term.width, minTermW)

	inputH := max(m.composer.textarea.Height(), inputMinHeight)

	// gap [+ footer + input]; both are hidden while a panel replaces them.
	chromeH := m.gapHeight()
	if !m.inputBlocked() {
		chromeH += footerRows + inputH + styles.InputChromeV + styles.InputMarginB
	}
	th := max(m.term.height-chromeH, minTranscriptH)

	// Transcript region (pad + content + pad) spans the full width.
	// The viewport spans the whole region so user bubbles run edge to edge;
	// every other message insets itself by ContentInset (textW).
	contentW := max(w, minInputInnerW+2*styles.ContentInset)

	// Input spans the full width.
	// lipgloss v2 Width includes padding → textarea = boxW - pad.
	inputInnerW := max(w-styles.InputChromeH, minInputInnerW)

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
// Remember if we were at the bottom before painting, then scroll back down if
// we started there.
func (m *Model) repaintTranscript() {
	m.transcript.invalidateMainView()
	if len(m.transcript.messages) == 0 {
		m.layout()
		m.transcript.viewport.SetContent("")
		return
	}

	stickBottom := m.transcript.viewport.AtBottom()
	m.layout()
	m.setTranscriptContent()
	if stickBottom {
		m.transcript.viewport.GotoBottom()
	}
}
