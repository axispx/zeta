package tui

import (
	"image/color"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/styles"
)

// The composer: its state, the textarea editor, and the editor's chrome. Draft
// images, prompt history, and submission live in attach.go / prompt_history.go /
// commands.go; this file owns the composer itself.

// composer is the input editor and everything staged in it.
type composer struct {
	textarea      textarea.Model
	promptHist    promptHistory     // up/down recall of prior user turns
	pendingImages map[int]image.Ref // draft images keyed by stable [Image N] id
	nextImageN    int               // last allocated token number (never renumbered)
}

// applyTextareaStyles sets textarea chrome; bg nil skips panel fill (pre-BackgroundColorMsg).
// Empty input dims the focused prompt arrow.
func applyTextareaStyles(ta *textarea.Model, bg color.Color) {
	ts := textarea.DefaultStyles(true)
	base := lipgloss.NewStyle()
	prompt := styles.Prompt
	ph := styles.Placeholder
	if bg != nil {
		base = base.Background(bg)
		prompt = prompt.Background(bg)
		ph = ph.Background(bg)
	}
	focusedPrompt := prompt
	if ta.Value() == "" {
		focusedPrompt = prompt.Faint(true)
	}
	ts.Focused.Base = base
	ts.Focused.Text = base
	ts.Focused.CursorLine = base
	ts.Focused.Placeholder = ph
	ts.Focused.Prompt = focusedPrompt
	ts.Blurred.Base = base
	ts.Blurred.Text = base
	ts.Blurred.CursorLine = base
	ts.Blurred.Placeholder = ph
	ts.Blurred.Prompt = prompt.Faint(true)
	ts.Cursor.Color = styles.White
	ts.Cursor.Blink = false
	ta.SetStyles(ts)
}

func (m *Model) syncTextareaStyles() {
	applyTextareaStyles(&m.composer.textarea, m.term.chrome.Input)
}
