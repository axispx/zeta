package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/tools"
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

	m.updating = true
	if got := m.busyLabel(); got != statusUpdating {
		t.Fatalf("updating = %q, want %q", got, statusUpdating)
	}
	m.updating = false

	m.turn = &turnSession{streaming: false, activeTool: -1}
	if got := m.busyLabel(); got != statusWaiting {
		t.Fatalf("pre-delta = %q, want %q", got, statusWaiting)
	}

	m.turn.thinking = "ponder"
	if got := m.busyLabel(); got != statusThinking {
		t.Fatalf("thinking = %q, want %q", got, statusThinking)
	}
	m.turn.thinking = ""

	m.turn.streaming = true
	if got := m.busyLabel(); got != statusWorking {
		t.Fatalf("streaming = %q, want %q", got, statusWorking)
	}

	m.messages = []Message{{Role: RoleTool, Tool: tools.Read}}
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
	// blank + spinner + blank
	if h := lipgloss.Height(got); h != busyStatusRows {
		t.Fatalf("busy status height=%d, want %d: %q", h, busyStatusRows, got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != busyStatusRows || strings.TrimSpace(lines[0]) != "" || strings.TrimSpace(lines[2]) != "" {
		t.Fatalf("expected blank padding rows: %q", got)
	}
	m.messages = []Message{{Role: RoleTool, Tool: tools.Read}}
	m.turn.activeTool = 0
	got = stripANSI(m.turnStatusLine())
	if !strings.Contains(got, statusReading) {
		t.Fatalf("busy status missing Reading: %q", got)
	}
}

// Long history sticks the newest user bubble to the viewport bottom. When the
// busy gap grows, layout() must shrink the viewport (and keep stick-to-bottom)
// so the bubble text stays visible — not clip painted lines after the fact.
func TestLayoutBusyGapKeepsBottomUserMessage(t *testing.T) {
	m := testModel()
	m.width = 60
	m.height = 24
	// Enough agent lines that the transcript overflows the viewport.
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("history line %02d", i))
	}
	m.messages = []Message{
		{Role: RoleAgent, Text: strings.Join(lines, "\n\n")},
		{Role: RoleUser, Text: "UNIQUE_USER_PROMPT"},
	}
	m.layout()
	m.setTranscriptContent()
	if !m.viewport.AtBottom() {
		t.Fatal("expected stick-to-bottom after setTranscriptContent")
	}
	idleH := m.viewport.Height()
	if m.gapHeight() != 1 { // styles.GapBeforeInput
		t.Fatalf("idle gapHeight=%d, want 1", m.gapHeight())
	}

	m.turn = &turnSession{streaming: false, activeTool: -1}
	m.spinner = spinner.New(spinner.WithSpinner(spinner.MiniDot))
	if m.gapHeight() != busyStatusRows {
		t.Fatalf("busy gapHeight=%d, want %d", m.gapHeight(), busyStatusRows)
	}
	m.layoutPreservingBottom()
	if m.viewport.Height() >= idleH {
		t.Fatalf("busy viewport height %d should be < idle %d", m.viewport.Height(), idleH)
	}
	if !m.viewport.AtBottom() {
		t.Fatal("expected stick-to-bottom after layoutPreservingBottom")
	}

	got := stripANSI(m.mainView())
	if !strings.Contains(got, "UNIQUE_USER_PROMPT") {
		t.Fatalf("user message missing from transcript with busy gap:\n%s", got)
	}
}

func TestGapHeight(t *testing.T) {
	m := testModel()
	m.width = 60
	if got := m.gapHeight(); got != 1 {
		t.Fatalf("idle gapHeight=%d, want 1", got)
	}
	m.turn = &turnSession{streaming: false, activeTool: -1}
	if got := m.gapHeight(); got != busyStatusRows {
		t.Fatalf("busy gapHeight=%d, want %d", got, busyStatusRows)
	}
	m.turn = nil
	m.compacting = true
	if got := m.gapHeight(); got != busyStatusRows {
		t.Fatalf("compacting gapHeight=%d, want %d", got, busyStatusRows)
	}
}

func TestGapContentKeepsStatusWithOverlay(t *testing.T) {
	// Status gap stays in-flow; floating overlay does not replace it.
	m := testModel()
	m.width = 60
	m.spinner = spinner.New(spinner.WithSpinner(spinner.MiniDot))
	m.turn = &turnSession{streaming: false, activeTool: -1}
	m.overlay.mode = overlayFiles
	m.overlay.files.matches = []string{"a.go"}

	gap := stripANSI(m.gapContent())
	if !strings.Contains(gap, statusWaiting) {
		t.Fatalf("status gap missing Waiting under overlay: %q", gap)
	}
	if m.gapHeight() != busyStatusRows {
		t.Fatalf("gapHeight=%d want %d", m.gapHeight(), busyStatusRows)
	}
	if !m.filterOverlayOpen() {
		t.Fatal("expected filter overlay open")
	}

	// Idle + overlay: blank gap stays reserved (no transcript jump).
	m.turn = nil
	if m.gapHeight() != 1 {
		t.Fatalf("idle+overlay gapHeight=%d want 1", m.gapHeight())
	}
}

func TestToolStatus(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{tools.Read, statusReading},
		{tools.Skill, statusReading},
		{tools.Edit, statusEditing},
		{tools.Write, statusEditing},
		{tools.Bash, statusRunning},
		{tools.Grep, statusSearching},
		{tools.Glob, statusSearching},
		{tools.WebSearch, statusSearching},
		{tools.WebFetch, statusFetching},
		{"", statusWorking},
		{"custom", statusWorking},
	}
	for _, tt := range tests {
		if got := toolStatus(tt.name); got != tt.want {
			t.Errorf("toolStatus(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
