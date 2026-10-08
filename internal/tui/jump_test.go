package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func scrolledUpModel(t *testing.T) *Model {
	t.Helper()
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("history line %02d", i))
	}
	m.transcript.messages = []Message{{Role: RoleAgent, Text: strings.Join(lines, "\n\n")}}
	m.repaintTranscript()
	return m
}

func TestJumpPillOnlyWhileScrolledUp(t *testing.T) {
	m := scrolledUpModel(t)
	if m.jumpVisible() {
		t.Fatal("no pill at the bottom")
	}
	m.transcript.viewport.ScrollUp(10)
	if !m.jumpVisible() {
		t.Fatal("pill expected while scrolled up")
	}
	surface := strings.Repeat("x\n", m.transcript.viewport.Height()+1) + "x"
	if got := stripANSI(m.withJumpButton(surface)); !strings.Contains(got, strings.TrimSpace(jumpLabel)) {
		t.Fatalf("pill not painted:\n%s", got)
	}
}

func TestJumpClickScrollsToBottom(t *testing.T) {
	m := scrolledUpModel(t)
	m.transcript.viewport.ScrollUp(10)
	row, x0, x1 := m.jumpBounds()

	// Outside the pill, or the wrong button, does nothing.
	if m.handleJumpClick(tea.MouseClickMsg{Button: tea.MouseLeft, X: x0 - 1, Y: row}) ||
		m.handleJumpClick(tea.MouseClickMsg{Button: tea.MouseLeft, X: x1, Y: row}) ||
		m.handleJumpClick(tea.MouseClickMsg{Button: tea.MouseRight, X: x0, Y: row}) {
		t.Fatal("click off the pill must pass through")
	}
	if m.transcript.viewport.AtBottom() {
		t.Fatal("nothing should have scrolled")
	}

	if !m.handleJumpClick(tea.MouseClickMsg{Button: tea.MouseLeft, X: x0, Y: row}) {
		t.Fatal("click on the pill should be consumed")
	}
	if !m.transcript.viewport.AtBottom() {
		t.Fatal("expected the transcript at the bottom")
	}
	if m.jumpVisible() {
		t.Fatal("pill should disappear once at the bottom")
	}
}

// While a turn runs the pill sits on the spinner row, not the transcript's
// last row, so it reads as part of the status.
func TestJumpPillOnStatusRowWhileBusy(t *testing.T) {
	m := scrolledUpModel(t)
	m.transcript.viewport.ScrollUp(10)
	idle, _, _ := m.jumpBounds()
	m.turn.current = &turnSession{activeTool: -1}
	row, _, _ := m.jumpBounds()
	if want := m.transcript.viewport.Height() + 1; row != want {
		t.Fatalf("busy pill row=%d, want spinner row %d", row, want)
	}
	if row <= idle-1 {
		t.Fatalf("busy row %d should be below the transcript's last row", row)
	}
}
