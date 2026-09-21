package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/axispx/zeta/internal/tools"
)

// Rows from the reported session: two wrapped commands in one tool run.
const (
	wrapCmdGitLog = "bash cd ~/Developer/zeta && git log --format='%h %ad %s' --date=short -- go.mod | head -20"
	wrapCmdDiff   = "bash cd ~/Developer/zeta && git diff --stat && gofmt -l ./internal ./cmd && go test ./internal/tui/ ./internal/... 2>&1 | tail -5"
)

func wrapRows() []Message {
	return []Message{
		{Role: RoleTool, Tool: tools.Bash, Status: ToolOK,
			Text: wrapCmdGitLog,
			Out:  "3f57b52 2026-07-23 Fix markdown render for unfenced code blocks\nbcdf850 2026-07-23 Initial commit\nexit: 0"},
		{Role: RoleTool, Tool: tools.Bash, Status: ToolOK,
			Text: wrapCmdDiff,
			Out:  "ok  \tgithub.com/axispx/zeta/internal/workspace\t(cached)\nexit: 0"},
	}
}

// TestToolGroupRowsFitWidth is the regression guard for a doubled transcript:
// no rendered row may exceed the wrap width. lipgloss's word wrap keeps the
// space it broke on, so a wrapped row can come back one cell over; Width()
// then pads the whole block to that row, and the viewport (SoftWrap) wraps
// every row that overflows, adding a blank display row under each line.
func TestToolGroupRowsFitWidth(t *testing.T) {
	for w := 20; w <= 160; w++ {
		body := renderToolGroup(wrapRows(), w, 0)
		for _, line := range strings.Split(body, "\n") {
			if got := ansi.StringWidth(line); got > w {
				t.Fatalf("width %d: rendered row is %d cells: %q", w, got, stripANSI(line))
			}
		}
	}
}

// transcriptAt builds a laid-out transcript showing rows at terminal width w.
func transcriptAt(w int, rows []Message) Model {
	m := testModel()
	m.term.width = w
	m.term.height = 40
	m.transcript.messages = rows
	m.layout()
	m.refreshTranscript()
	return m
}

// overflowRows is the adversarial companion to wrapRows: one row per transcript
// kind, each mixing prose, an unbreakable token, and a width-hostile construct.
// Rows render through different styles (user padding, glamour, diff colouring,
// plan framing), so each is a separate way to miss the wrap width.
func overflowRows() []Message {
	long := "https://example.com/very/long/url/that/never/breaks/anywhere/at/all?q=1&r=2"
	return []Message{
		{Role: RoleUser, Text: strings.Repeat("word ", 40) + long},
		{Role: RoleAgent, Text: "Prose " + strings.Repeat("word ", 30) +
			"\n\n- " + strings.Repeat("x", 300) + "\n\n`" + strings.Repeat("y", 200) + "`"},
		{Role: RoleAgent, Text: "| a | b |\n|---|---|\n| " + strings.Repeat("cell ", 40) + " | z |\n"},
		{Role: RoleAgent, Text: "## Plan\n\n- do this\n\n```go\n" + strings.Repeat("fmt.Println(\"x\")", 20) + "\n```"},
		{Role: RoleTool, Tool: tools.Bash, Text: "bash " + strings.Repeat("z", 120), Out: "ok"},
		{Role: RoleError, Text: strings.Repeat("boom ", 60)},
		{Role: RoleSystem, Text: strings.Repeat("sys ", 60)},
	}
}

// TestTranscriptRowsDoNotSoftWrap is the end-to-end guard: no transcript line
// may exceed contentW, so the viewport never has to wrap or cut one, so its
// display rows equal its content lines and drag selection (which re-derives
// rows with wrapContentLines) addresses the same cells the user sees.
func TestTranscriptRowsDoNotSoftWrap(t *testing.T) {
	fixtures := map[string][]Message{"tool-run": wrapRows(), "overflow": overflowRows()}
	for name, rows := range fixtures {
		for live := range 2 {
			for w := 20; w <= 160; w++ {
				m := transcriptAt(w, rows)
				if live == 1 {
					m.turn.current = &turnSession{
						cancel: func() {}, activeTool: -1,
						streaming: true,
						thinking:  strings.Repeat("thinking about ", 20) + "x",
					}
					m.transcript.messages = append(m.transcript.messages, Message{
						Role: RoleAgent, Text: "live " + strings.Repeat("tok ", 30),
					})
					m.refreshTranscript()
				}
				content := m.transcript.viewport.GetContent()
				cw := m.transcript.contentW
				for _, line := range strings.Split(content, "\n") {
					if got := ansi.StringWidth(line); got > cw {
						t.Fatalf("%s (live=%d) width %d (contentW %d): transcript line is %d cells: %q",
							name, live, w, cw, got, stripANSI(line))
					}
				}
				lines := strings.Count(content, "\n") + 1
				if got := m.transcript.viewport.TotalLineCount(); got != lines {
					t.Fatalf("%s (live=%d) terminal width %d (contentW %d): %d content lines render as %d display rows",
						name, live, w, cw, lines, got)
				}
			}
		}
	}
}

// TestTranscriptWrapReportedWidth covers the widths from the report. At
// contentW 123 the git log command above wraps and used to come back 124 cells,
// which padded the whole run and doubled it: 7 content lines showed as 14
// display rows.
func TestTranscriptWrapReportedWidth(t *testing.T) {
	cases := []struct {
		termW   int // terminal width; contentW = termW - 2 when no scrollbar
		lines   int // content lines after lipgloss wrapping
		display int // display rows the viewport must show
	}{
		{termW: 123, lines: 7, display: 7},   // reported: was 14
		{termW: 124, lines: 7, display: 7},   //
		{termW: 125, lines: 13, display: 13}, // reported: longest wrapped row
		{termW: 126, lines: 7, display: 7},   //
	}
	for _, tc := range cases {
		m := transcriptAt(tc.termW, wrapRows())
		if m.transcript.showScrollbar {
			t.Fatalf("terminal width %d: expected no scrollbar for this fixture", tc.termW)
		}
		content := m.transcript.viewport.GetContent()
		if got := strings.Count(content, "\n") + 1; got != tc.lines {
			t.Errorf("terminal width %d: %d content lines, want %d", tc.termW, got, tc.lines)
		}
		if got := m.transcript.viewport.TotalLineCount(); got != tc.display {
			t.Errorf("terminal width %d (contentW %d): %d display rows, want %d",
				tc.termW, m.transcript.contentW, got, tc.display)
		}
	}
}
