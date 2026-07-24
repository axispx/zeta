package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
)

func TestBusyLabel(t *testing.T) {
	m := Model{}
	if got := m.busyLabel(); got != "" {
		t.Fatalf("idle = %q", got)
	}

	m.compacting = true
	if got := m.busyLabel(); got != statusCompacting {
		t.Fatalf("compacting = %q, want %q", got, statusCompacting)
	}
	m.compacting = false

	m.turn = &turnSession{streaming: false, activeTool: -1}
	if got := m.busyLabel(); got != statusWaiting {
		t.Fatalf("pre-delta = %q, want %q", got, statusWaiting)
	}

	m.turn.streaming = true
	if got := m.busyLabel(); got != statusWorking {
		t.Fatalf("streaming = %q, want %q", got, statusWorking)
	}

	m.messages = []Message{{Role: RoleTool, Tool: "read"}}
	m.turn.streaming = false
	m.turn.activeTool = 0
	if got := m.busyLabel(); got != statusReading {
		t.Fatalf("active tool = %q, want %q", got, statusReading)
	}
}

func TestTurnStatusLine(t *testing.T) {
	m := Model{
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
	if got := m.turnStatusLine(); got != "" {
		t.Fatalf("idle status = %q", got)
	}
	m.turn = &turnSession{streaming: false, activeTool: -1}
	got := stripANSI(m.turnStatusLine())
	if !strings.Contains(got, statusWaiting) {
		t.Fatalf("busy status missing Waiting: %q", got)
	}
	m.messages = []Message{{Role: RoleTool, Tool: "read"}}
	m.turn.activeTool = 0
	got = stripANSI(m.turnStatusLine())
	if !strings.Contains(got, statusReading) {
		t.Fatalf("busy status missing Reading: %q", got)
	}
}

func TestToolStatus(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"read", statusReading},
		{"edit", statusEditing},
		{"bash", statusRunning},
		{"grep", statusSearching},
		{"websearch", statusSearching},
		{"webfetch", statusFetching},
		{"", statusWorking},
		{"custom", statusWorking},
	}
	for _, tt := range tests {
		if got := toolStatus(tt.name); got != tt.want {
			t.Errorf("toolStatus(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
