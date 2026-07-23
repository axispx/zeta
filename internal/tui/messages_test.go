package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestToolRunAtGroupsTools(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Text: "hi"},
		{Role: RoleTool, Text: "read a.go", Tool: "read"},
		{Role: RoleTool, Text: "read b.go", Tool: "read"},
		{Role: RoleAgent, Text: "done"},
	}
	if toolRunAt(msgs, 0) != nil {
		t.Fatal("user is not a tool run")
	}
	run := toolRunAt(msgs, 1)
	if len(run) != 2 || run[0].Text != "read a.go" {
		t.Fatalf("tool run: %#v", run)
	}
	if toolRunAt(msgs, 3) != nil {
		t.Fatal("agent is not a tool run")
	}
}

func TestRenderToolGroupCollapses(t *testing.T) {
	msgs := []Message{
		{Role: RoleTool, Text: "read a.go", Tool: "read"},
		{Role: RoleTool, Text: "read b.go", Tool: "read"},
		{Role: RoleTool, Text: "read c.go", Tool: "read"},
		{Role: RoleTool, Text: "read d.go", Tool: "read"},
		{Role: RoleTool, Text: "edit e.go", Tool: "edit"},
	}
	out := renderToolGroup(msgs, 80, 0)
	if !strings.Contains(out, "Read, edited") {
		t.Fatalf("missing verb header: %q", out)
	}
	if !strings.Contains(out, "4 files") || !strings.Contains(out, "1 edit") {
		t.Fatalf("missing counts: %q", out)
	}
	if !strings.Contains(out, "2 earlier items hidden") {
		t.Fatalf("missing hidden line: %q", out)
	}
	if strings.Contains(out, "read a.go") {
		t.Fatalf("should hide earliest: %q", out)
	}
	if !strings.Contains(out, "read c.go") || !strings.Contains(out, "read d.go") || !strings.Contains(out, "edit e.go") {
		t.Fatalf("missing tail tools: %q", out)
	}
}

func TestRenderToolGroupBashSplits(t *testing.T) {
	msgs := []Message{
		{Role: RoleTool, Text: "read a.go", Tool: "read"},
		{Role: RoleTool, Text: "read b.go", Tool: "read"},
		{Role: RoleTool, Text: "bash go test ./...", Tool: "bash"},
		{Role: RoleTool, Text: "bash ls", Tool: "bash"},
		{Role: RoleTool, Text: `grep "x" .`, Tool: "grep"},
		{Role: RoleTool, Text: "edit c.go", Tool: "edit"},
		{Role: RoleTool, Text: "bash pwd", Tool: "bash"},
		{Role: RoleTool, Text: "read d.go", Tool: "read"},
	}
	out := renderToolGroup(msgs, 0, 0)
	if !strings.Contains(out, "Read") || !strings.Contains(out, "2 files") {
		t.Fatalf("missing first group: %q", out)
	}
	// Blank at cluster↔bash boundary; consecutive bash stack tightly.
	if !strings.Contains(out, "\n\n$ go test ./...\n$ ls\n\n") {
		t.Fatalf("expected stacked bash with blank borders: %q", out)
	}
	if !strings.Contains(out, "Edited, grepped") {
		t.Fatalf("missing regroup after bash: %q", out)
	}
	if !strings.Contains(out, "1 grep") || !strings.Contains(out, "1 edit") {
		t.Fatalf("missing regroup counts: %q", out)
	}
	if !strings.Contains(out, "\n\n$ pwd\n\n") {
		t.Fatalf("expected blank lines around trailing bash: %q", out)
	}
	if strings.Count(out, "Read") != 1 {
		t.Fatalf("unexpected header for single trailing read: %q", out)
	}
}

func TestRenderBashLineEmptyCommand(t *testing.T) {
	out := renderShellCall(Message{Role: RoleTool, Text: "bash", Tool: "bash"})
	if out != "$" {
		t.Fatalf("empty bash: %q", out)
	}
}

func TestRenderShellCallShowsLastLines(t *testing.T) {
	m := Message{
		Role: RoleTool,
		Text: "bash go test",
		Tool: "bash",
		Out:  "a\nb\nc\nd\nexit: 0",
	}
	tail := lastNonEmptyLines(stripOKExit(m.Out), maxBashOutLines)
	if tail != "b\nc\nd" {
		t.Fatalf("tail=%q", tail)
	}
	out := renderShellCall(m)
	if !strings.HasPrefix(out, "$ go test\n") {
		t.Fatalf("cmd: %q", out)
	}
	if strings.Contains(out, "exit: 0") {
		t.Fatalf("should strip ok exit: %q", out)
	}
	for _, line := range strings.Split(tail, "\n") {
		if !strings.Contains(out, line) {
			t.Fatalf("missing %q: %q", line, out)
		}
	}
}

func TestRenderShellCallKeepsErrorExit(t *testing.T) {
	m := Message{
		Role: RoleTool,
		Text: "bash false",
		Tool: "bash",
		Out:  "nope\nexit: 1",
	}
	out := renderShellCall(m)
	if !strings.Contains(out, "exit: 1") {
		t.Fatalf("expected error exit: %q", out)
	}
}

func TestRenderShellCallSkipsEmptyLines(t *testing.T) {
	m := Message{
		Role: RoleTool,
		Text: "bash echo",
		Tool: "bash",
		Out:  "a\n\n\nb\n\nc",
	}
	out := renderShellCall(m)
	if strings.Contains(out, "\n\n") {
		t.Fatalf("empty lines in output: %q", out)
	}
	tail := lastNonEmptyLines(m.Out, maxBashOutLines)
	if tail != "a\nb\nc" {
		t.Fatalf("tail=%q", tail)
	}
}

func TestStripOKExit(t *testing.T) {
	if got := stripOKExit("hello\nexit: 0"); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := stripOKExit("exit: 0"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := stripOKExit("nope\nexit: 1"); got != "nope\nexit: 1" {
		t.Fatalf("got %q", got)
	}
	if got := stripOKExit("x\nexit: 10"); got != "x\nexit: 10" {
		t.Fatalf("got %q", got)
	}
}

func TestLastNonEmptyLines(t *testing.T) {
	if got := lastNonEmptyLines("a\n\nb\n\n\nc\n", 2); got != "b\nc" {
		t.Fatalf("got %q", got)
	}
	if got := lastNonEmptyLines("\n\n", 3); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := lastNonEmptyLines("only", 3); got != "only" {
		t.Fatalf("got %q", got)
	}
}

func TestToolGroupHeader(t *testing.T) {
	verbs, counts := toolGroupHeader([]string{"grep", "grep", "read"})
	if verbs != "Grepped, read" {
		t.Fatalf("verbs=%q", verbs)
	}
	if counts != "2 greps, 1 file" {
		t.Fatalf("counts=%q", counts)
	}
}

func TestWidthBody(t *testing.T) {
	out := widthBody("hi", 10)
	if lipgloss.Width(out) != 10 {
		t.Fatalf("width = %d", lipgloss.Width(out))
	}
}

func TestStackMainChromeKeepsInputStable(t *testing.T) {
	main := strings.Repeat("m\n", 19) + "m" // 20 lines
	overlay := "a\nb\nc\nd\ne"              // 5 lines
	input, footer := "INPUT", "FOOTER"

	withGap := stackMainChrome(main, "", input, footer)
	withOverlay := stackMainChrome(main, overlay, input, footer)

	// Input should sit at the same absolute row in both stacks.
	gapIdx := strings.Index(withGap, "INPUT")
	overlayIdx := strings.Index(withOverlay, "INPUT")
	if gapIdx < 0 || overlayIdx < 0 {
		t.Fatal("missing INPUT")
	}
	if lipgloss.Height(withGap[:gapIdx]) != lipgloss.Height(withOverlay[:overlayIdx]) {
		t.Fatalf("input jumped: gap-stack h=%d overlay-stack h=%d",
			lipgloss.Height(withGap[:gapIdx]), lipgloss.Height(withOverlay[:overlayIdx]))
	}
}
