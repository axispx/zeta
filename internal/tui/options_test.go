package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestKeyOption(t *testing.T) {
	rows := []optionRow{{key: "a", label: "A"}, {key: "d", label: "D"}}
	if keyOption("a", rows) != 0 || keyOption("d", rows) != 1 || keyOption("x", rows) != -1 {
		t.Fatal("keys")
	}
}

func TestOptionListHandleKey(t *testing.T) {
	var o optionList
	o.setRows([]optionRow{{key: "a", label: "A"}, {key: "d", label: "D"}})
	if _, chose, handled := o.handleKey(tea.KeyPressMsg{Text: "down"}); !handled || chose || o.selected != 1 {
		t.Fatalf("down: sel=%d chose=%v handled=%v", o.selected, chose, handled)
	}
	if idx, chose, handled := o.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}); !handled || !chose || idx != 1 {
		t.Fatalf("enter: idx=%d chose=%v", idx, chose)
	}
	o.selected = 0
	if idx, chose, handled := o.handleKey(tea.KeyPressMsg{Code: 'a', Text: "a"}); !handled || !chose || idx != 0 {
		t.Fatalf("hotkey: idx=%d", idx)
	}
	if _, _, handled := o.handleKey(tea.KeyPressMsg{Text: "esc"}); handled {
		t.Fatal("esc must not be handled")
	}
}
