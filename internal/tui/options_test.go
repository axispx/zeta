package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/styles"
)

func TestMoveOption(t *testing.T) {
	if got := moveOption(0, 3, -1); got != 0 {
		t.Fatalf("clamp low: %d", got)
	}
	if got := moveOption(2, 3, 1); got != 2 {
		t.Fatalf("clamp high: %d", got)
	}
	if got := moveOption(1, 3, 1); got != 2 {
		t.Fatalf("down: %d", got)
	}
}

func TestDigitOption(t *testing.T) {
	if digitOption("1", 3) != 0 || digitOption("3", 3) != 2 {
		t.Fatal("digits")
	}
	if digitOption("4", 3) != -1 || digitOption("a", 3) != -1 {
		t.Fatal("out of range")
	}
}

func TestOptionListHandleKey(t *testing.T) {
	var o optionList
	o.setRows([]optionRow{{label: "A"}, {label: "D"}})
	if _, chose, handled := o.handleKey(tea.KeyPressMsg{Text: "down"}); !handled || chose || o.selected != 1 {
		t.Fatalf("down: sel=%d chose=%v handled=%v", o.selected, chose, handled)
	}
	if idx, chose, handled := o.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}); !handled || !chose || idx != 1 {
		t.Fatalf("enter: idx=%d chose=%v", idx, chose)
	}
	o.selected = 0
	if idx, chose, handled := o.handleKey(tea.KeyPressMsg{Code: '2', Text: "2"}); !handled || chose || idx != 1 || o.selected != 1 {
		t.Fatalf("row number: idx=%d sel=%d chose=%v", idx, o.selected, chose)
	}
	// A letter is swallowed without moving or deciding.
	if idx, chose, handled := o.handleKey(tea.KeyPressMsg{Code: 'a', Text: "a"}); !handled || chose || idx != 1 {
		t.Fatalf("letter must not decide: idx=%d chose=%v handled=%v", idx, chose, handled)
	}
	if _, _, handled := o.handleKey(tea.KeyPressMsg{Text: "esc"}); handled {
		t.Fatal("esc must not be handled")
	}
}

func TestRenderOptionRowsHintBelowLabel(t *testing.T) {
	ink := styles.PlainOverlayInk()
	rows := []optionRow{
		{label: "First", hint: "What first means"},
		{label: "Second", hint: ""},
	}
	lines := strings.Split(strings.TrimPrefix(stripANSI(renderOptionRows(rows, 0, 60, ink)), "\n"), "\n")
	want := []string{
		"→ 1. First",
		"     What first means",
		"  2. Second",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines=%q", lines)
	}
	for i, w := range want {
		if strings.TrimRight(lines[i], " ") != w {
			t.Fatalf("line %d = %q, want %q", i, lines[i], w)
		}
	}
}

func TestOptionHintLinesWrapAndIndent(t *testing.T) {
	lines := optionHintLines(optionRow{hint: "An error, diff, or excerpt that did not fit the message"}, 40)
	if len(lines) < 2 {
		t.Fatalf("expected wrap: %q", lines)
	}
	// Bodies arrive unindented; the renderer owns the gutter.
	for _, l := range lines {
		if strings.HasPrefix(l, " ") {
			t.Fatalf("body must be unindented: %q", l)
		}
	}
	if got := optionHintLines(optionRow{}, 40); got != nil {
		t.Fatalf("empty hint: %q", got)
	}
	if got := optionHintLines(optionRow{hint: "tiny"}, 4); len(got) != 1 {
		t.Fatalf("narrow term must not wrap: %q", got)
	}
}

// A row's description lines belong to that row, so clicking one selects it.
func TestOptionListRowAtLineSpansHint(t *testing.T) {
	var o optionList
	o.setRows([]optionRow{
		{label: "First", hint: "one"},
		{label: "Second", hint: "two"},
	})
	// label · description, twice.
	for line, want := range map[int]int{0: 0, 1: 0, 2: 1, 3: 1} {
		if got := o.rowAtLine(line, 60); got != want {
			t.Fatalf("line %d = %d, want %d", line, got, want)
		}
	}
	if got := o.rowAtLine(4, 60); got != -1 {
		t.Fatalf("past end = %d", got)
	}
	if got := o.rowAtLine(-1, 60); got != -1 {
		t.Fatalf("above list = %d", got)
	}
}

func TestRenderOptionRowsMultilineLabel(t *testing.T) {
	ink := styles.PlainOverlayInk()
	rows := labeledRows([]string{"Answer"}, []string{"a wrapped\nsecond line\nthird line"})
	out := stripANSI(renderOptionRows(rows, 0, 60, ink))
	lines := strings.Split(strings.TrimPrefix(out, "\n"), "\n")
	// label · three hint lines.
	if len(lines) != 4 {
		t.Fatalf("lines=%q", lines)
	}
	if got, want := strings.TrimRight(lines[0], " "), "→ 1. Answer"; got != want {
		t.Fatalf("label line = %q, want %q", got, want)
	}
	for i, want := range []string{"a wrapped", "second line", "third line"} {
		if col := runeCol(lines[i+1], want); col != optionHintIndent {
			t.Fatalf("continuation %d starts at col %d, want %d: %q", i, col, optionHintIndent, lines[i+1])
		}
	}
}

func TestRenderOptionRowsFreeformLabel(t *testing.T) {
	ink := styles.PlainOverlayInk()
	rows := labeledRows([]string{"First", "Other"}, []string{"a description", ""})
	rows[1].label, rows[1].labelCursor = "my typed answer", true
	lines := strings.Split(strings.TrimPrefix(stripANSI(renderOptionRows(rows, 1, 60, ink)), "\n"), "\n")
	// First row: label · description. Other: label only.
	if len(lines) != 3 {
		t.Fatalf("Other must not add a description line: %q", lines)
	}
	if got, want := strings.TrimRight(lines[2], " "), "→ 2. my typed answer"+optionCaret; got != want {
		t.Fatalf("freeform row = %q, want %q", got, want)
	}
}

// runeCol is the display column of sub within s.
func runeCol(s, sub string) int {
	i := strings.Index(s, sub)
	if i < 0 {
		return -1
	}
	return utf8.RuneCountInString(s[:i])
}
