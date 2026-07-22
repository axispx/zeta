package tui

import (
	"strings"
	"testing"
)

func TestClipBottomLines(t *testing.T) {
	if got := clipBottomLines("a\nb\nc", 1); got != "a\nb" {
		t.Fatalf("clip 1 = %q", got)
	}
	if got := clipBottomLines("a\nb", 2); got != "" {
		t.Fatalf("clip all = %q", got)
	}
}

func TestFormatPaletteRow(t *testing.T) {
	items := []command{{"/clear", "start a new session"}, {"/resume", "open a previous session"}}
	nameW := paletteNameWidth(items)

	row := formatPaletteRow(nameW, items[0], true)
	if !strings.Contains(row, "→") || !strings.Contains(row, "/clear") {
		t.Fatalf("selected row = %q", row)
	}
	if !strings.Contains(row, "start a new session") {
		t.Fatalf("missing desc: %q", row)
	}

	r1 := formatPaletteRow(nameW, items[0], false)
	r2 := formatPaletteRow(nameW, items[1], false)
	i1 := strings.Index(r1, "start")
	i2 := strings.Index(r2, "open")
	if i1 <= 0 || i1 != i2 {
		t.Fatalf("desc columns misaligned: %d vs %d\n%q\n%q", i1, i2, r1, r2)
	}
}
