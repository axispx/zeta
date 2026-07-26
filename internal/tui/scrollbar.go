package tui

import (
	"strings"

	"github.com/axispx/zeta/internal/styles"
)

const scrollbarWidth = 1

// renderScrollbar draws a 1-column vertical scrollbar when content overflows.
// height is the track size in rows; total is content lines; yOffset is the scroll position.
func renderScrollbar(height, total, yOffset int) string {
	if height <= 0 || total <= height {
		return ""
	}

	track := make([]string, height)
	for i := range track {
		track[i] = " "
	}

	// Thumb size ∝ visible/total, at least 1 row.
	thumbH := max(1, height*height/total)
	if thumbH > height {
		thumbH = height
	}
	maxOff := total - height
	travel := height - thumbH
	thumbTop := 0
	if maxOff > 0 && travel > 0 {
		thumbTop = (yOffset*travel + maxOff/2) / maxOff
	}
	if thumbTop < 0 {
		thumbTop = 0
	}
	if thumbTop+thumbH > height {
		thumbTop = height - thumbH
	}
	for i := thumbTop; i < thumbTop+thumbH; i++ {
		track[i] = styles.ScrollThumb.Render("█")
	}

	return strings.Join(track, "\n")
}
