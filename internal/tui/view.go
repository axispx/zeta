package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

// This file is the frame: how the regions are composed and stacked, plus the
// terminal capabilities the program announces. It reads state and renders.

func (m Model) View() tea.View {
	if m.exit.quitting {
		return tea.NewView("")
	}
	if !m.term.ready {
		return m.programView(styles.SystemMsg.Render("loading…"))
	}
	if m.picker.active {
		w, h := m.term.width, m.term.height
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		return m.programView(m.renderPicker(w, h))
	}
	if m.config.active {
		w, h := m.term.width, m.term.height
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		return m.programView(m.config.View(m.term.chrome, w, w, h))
	}

	// Stack: (transcript + gap [+ floating overlay]) | input? | footer.
	// Filter overlays pin over the bottom of main+gap so the list sits flush on
	// the input without resizing the viewport. Status stays painted in the gap
	// rows (covered only where the list overlaps).
	// layout() already sized the transcript for gapHeight().
	// Empty gap ("") is still one JoinVertical row (idle blank spacer).
	surface := lipgloss.JoinVertical(lipgloss.Left, m.mainView(), m.gapContent())
	if m.filterOverlayOpen() {
		if ov := m.renderOverlay(m.term.width); ov != "" {
			surface = pinOverlayBottom(surface, ov)
		}
	}
	return m.programView(stackMainChrome(surface, m.renderInput(), m.renderFooter()))
}

// pinOverlayBottom draws overlay over the bottom of main without growing layout.
// main is truncated from the bottom to make room; rows below main stay put.
func pinOverlayBottom(main, overlay string) string {
	if overlay == "" {
		return main
	}
	ovH := lipgloss.Height(overlay)
	if ovH < 1 {
		return main
	}
	mainH := lipgloss.Height(main)
	if mainH < 1 {
		return overlay
	}
	if ovH >= mainH {
		return lipgloss.NewStyle().MaxHeight(mainH).Height(mainH).Render(overlay)
	}
	topH := mainH - ovH
	top := lipgloss.NewStyle().MaxHeight(topH).Height(topH).Render(main)
	return lipgloss.JoinVertical(lipgloss.Left, top, overlay)
}

// renderInput returns the input box, or "" when a panel replaces it.
func (m Model) renderInput() string {
	if m.inputBlocked() {
		return ""
	}
	inputW := max(m.term.width-2*styles.InputMarginH, minInputInnerW+styles.InputChromeH)
	inputH := max(m.composer.textarea.Height(), inputMinHeight)

	input := m.term.chrome.InputBox().Width(inputW).Height(inputH + styles.InputPadV).Render(m.composer.textarea.View())
	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH, styles.InputMarginB, styles.InputMarginH).
		Render(input)
}

func (m Model) renderFooter() string {
	footerW := max(m.term.width-2*styles.InputMarginH, 1)

	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH).
		Render(inputFooter(footerW, m.session.WS, m.session.Cfg, m.session.Mode, m.session.ContextTokens, m.transcript.sessionDiff))
}

// stackMainChrome places the main surface (transcript [+ gap] [+ pinned overlay]),
// optional input, and footer. Gap is folded into main by View before calling.
// Omit input when a panel replaces the composer.
func stackMainChrome(main, input, footer string) string {
	if input == "" {
		return lipgloss.JoinVertical(lipgloss.Left, main, footer)
	}
	return lipgloss.JoinVertical(lipgloss.Left, main, input, footer)
}

func (m Model) programView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	// Enables shift+enter and other modified keys on supporting terminals.
	v.KeyboardEnhancements.ReportEventTypes = true
	// Bubble Tea v2 maps WindowTitle → OSC 2.
	v.WindowTitle = terminalTitle(m.session.Log)
	return v
}
