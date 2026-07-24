package tui

import (
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

const (
	maxThinkingLines = 4    // live reasoning visual lines shown in the transcript
	maxThinkingBytes = 2048 // cap stored reasoning so long CoT cannot grow unbounded
)

// appendThinking concatenates a reasoning delta and keeps only the recent tail.
func appendThinking(prev, delta string) string {
	s := prev + delta
	if len(s) <= maxThinkingBytes {
		return s
	}
	s = s[len(s)-maxThinkingBytes:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// renderThinkingTail wraps reasoning text and keeps only the latest dim lines.
func renderThinkingTail(text string, width int) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	wrap := lipgloss.NewStyle()
	if width > 0 {
		wrap = wrap.Width(width)
	}
	lines := strings.Split(wrap.Render(text), "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > maxThinkingLines {
		lines = lines[len(lines)-maxThinkingLines:]
	}
	return styles.ThinkingMsg.Render(strings.Join(lines, "\n"))
}

// writeThinkingTail appends the live reasoning tail into the transcript builder.
func writeThinkingTail(b *strings.Builder, text string, width int) {
	tail := renderThinkingTail(text, width)
	if tail == "" {
		return
	}
	if b.Len() == 0 {
		// Empty transcript: match message top margin.
		tail = lipgloss.NewStyle().MarginTop(1).Render(tail)
	} else {
		b.WriteString("\n\n")
	}
	b.WriteString(tail)
}
