package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

func TestRenderAgentSettledFramesPlan(t *testing.T) {
	msg := &Message{
		Role: RoleAgent,
		Text: "Intro.\n\n<proposed_plan>\n## Fix auth\n\n- step one\n</proposed_plan>",
	}
	out := stripANSI(msg.renderBody(80, lipgloss.NewStyle(), false))
	if strings.Contains(out, "<proposed_plan>") {
		t.Fatalf("tags leaked: %q", out)
	}
	if !strings.Contains(out, "Fix auth") || !strings.Contains(out, "step one") {
		t.Fatalf("missing plan body: %q", out)
	}
	if !strings.Contains(out, "┃") {
		t.Fatalf("expected plan frame border, got %q", out)
	}
}

func TestRenderAgentLiveFramesOpenPlan(t *testing.T) {
	msg := &Message{
		Role: RoleAgent,
		Text: "Intro.\n\n<proposed_plan>\n## Fix auth\npartial",
	}
	out := stripANSI(msg.renderBody(80, lipgloss.NewStyle(), true))
	if strings.Contains(out, "<proposed_plan>") {
		t.Fatalf("tags leaked live: %q", out)
	}
	if !strings.Contains(out, "Fix auth") || !strings.Contains(out, "┃") {
		t.Fatalf("expected framed live plan: %q", out)
	}
}

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
		{Role: RoleTool, Text: "grep x", Tool: "grep"},
	}
	out := renderToolGroup(msgs, 80, 0)
	if !strings.Contains(out, "Read, grepped") {
		t.Fatalf("missing verb header: %q", out)
	}
	if !strings.Contains(out, "4 files") || !strings.Contains(out, "1 grep") {
		t.Fatalf("missing counts: %q", out)
	}
	if !strings.Contains(out, "2 earlier items hidden") {
		t.Fatalf("missing hidden line: %q", out)
	}
	if strings.Contains(out, "read a.go") {
		t.Fatalf("should hide earliest: %q", out)
	}
	if !strings.Contains(out, "read c.go") || !strings.Contains(out, "read d.go") || !strings.Contains(out, "grep x") {
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
		{Role: RoleTool, Text: "edit c.go", Tool: "edit", Status: ToolOK, Out: "--- c.go\n+++ c.go\n@@ -1 +1 @@\n-old\n+new\n"},
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
	if !strings.Contains(out, "Grepped") || !strings.Contains(out, "grep \"x\"") {
		t.Fatalf("missing grep after bash: %q", out)
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "Edited") || !strings.Contains(plain, "c.go") {
		t.Fatalf("missing edit header: %q", plain)
	}
	if !strings.Contains(plain, "- old") || !strings.Contains(plain, "+ new") {
		t.Fatalf("missing guttered edit diff: %q", plain)
	}
	if !strings.Contains(out, "\n\n$ pwd\n\n") {
		t.Fatalf("expected blank lines around trailing bash: %q", out)
	}
	if !strings.Contains(plain, "Read  1 file") {
		t.Fatalf("missing single-tool verb for trailing read: %q", plain)
	}
}

func TestRenderToolClusterSingleShowsVerb(t *testing.T) {
	out := stripANSI(renderToolGroup([]Message{
		{Role: RoleTool, Text: "read a.go", Tool: "read"},
	}, 0, 0))
	if !strings.HasPrefix(out, "Read  1 file\nread a.go") {
		t.Fatalf("single tool: %q", out)
	}
}

func TestRenderEditCall(t *testing.T) {
	m := Message{
		Role:   RoleTool,
		Text:   "edit hello.txt",
		Tool:   "edit",
		Status: ToolOK,
		Out:    "--- hello.txt\n+++ hello.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n",
	}
	out := renderEditCall(m)
	plain := stripANSI(out)
	if !strings.HasPrefix(plain, "Edited  hello.txt  +1  -1\n") {
		t.Fatalf("header: %q", plain)
	}
	if strings.Contains(out, "---") || strings.Contains(out, "+++") || strings.Contains(plain, "@@") {
		t.Fatalf("should omit file/hunk headers: %q", plain)
	}
	if !strings.Contains(plain, "- two") || !strings.Contains(plain, "+ TWO") {
		t.Fatalf("missing guttered hunk lines: %q", plain)
	}
	if !strings.Contains(plain, "  one") || !strings.Contains(plain, "  three") {
		t.Fatalf("missing guttered context: %q", plain)
	}
	// Markdown-style list content must not glue into "+-" / "--".
	list := stripANSI(renderUnifiedDiff("@@ -1 +1 @@\n-- item\n+- item\n"))
	if strings.Contains(list, "@@") {
		t.Fatalf("should omit hunk header: %q", list)
	}
	if strings.Contains(list, "+-") || strings.Contains(list, "-- item") {
		t.Fatalf("gutter glued to content: %q", list)
	}
	if !strings.Contains(list, "+ - item") || !strings.Contains(list, "- - item") {
		t.Fatalf("expected spaced gutter: %q", list)
	}
}

func TestRenderEditCallNoDiff(t *testing.T) {
	out := stripANSI(renderEditCall(Message{Role: RoleTool, Text: "edit x", Tool: "edit", Status: ToolOK}))
	if out != "Edited  x" {
		t.Fatalf("got %q", out)
	}
	// Empty-file create / no-op: tool returns "" — never prose in Out.
	out = stripANSI(renderEditCall(Message{Role: RoleTool, Text: "create empty.txt", Tool: "edit", Status: ToolOK, Out: ""}))
	if out != "Created  empty.txt" {
		t.Fatalf("empty create: %q", out)
	}
}

func TestRenderEditCallPendingVerbs(t *testing.T) {
	cases := []struct {
		text, want string
	}{
		{"edit a.go", "Editing  a.go"},
		{"create a.go", "Creating  a.go"},
		{"write a.go", "Writing  a.go"},
	}
	for _, tc := range cases {
		out := stripANSI(renderEditCall(Message{Role: RoleTool, Text: tc.text, Tool: "edit"}))
		if !strings.HasPrefix(out, tc.want) {
			t.Fatalf("%q: got %q, want prefix %q", tc.text, out, tc.want)
		}
	}
}

func TestRenderEditCallCreated(t *testing.T) {
	out := stripANSI(renderEditCall(Message{
		Role:   RoleTool,
		Text:   "create hello.txt",
		Tool:   "edit",
		Status: ToolOK,
		Out:    "--- hello.txt\n+++ hello.txt\n@@ -0,0 +1 @@\n+hi\n",
	}))
	if !strings.HasPrefix(out, "Created  hello.txt  +1\n") {
		t.Fatalf("got %q", out)
	}
}

func TestRenderEditCallBasename(t *testing.T) {
	out := stripANSI(renderEditCall(Message{
		Role:   RoleTool,
		Text:   "edit /Users/ashish/Developer/zeta/CONTRIBUTORS.md",
		Tool:   "edit",
		Status: ToolOK,
		Out:    "--- a\n+++ b\n@@ -1,2 +1,3 @@\n a\n-b\n+c\n+d\n",
	}))
	first, _, _ := strings.Cut(out, "\n")
	if first != "Edited  CONTRIBUTORS.md  +2  -1" {
		t.Fatalf("got %q", first)
	}
	if strings.Contains(first, "/") {
		t.Fatalf("should be basename only: %q", first)
	}
}

func TestCountDiffLines(t *testing.T) {
	adds, dels := countDiffLines("--- a\n+++ b\n@@ -1 +1,2 @@\n-old\n+new\n+extra\n")
	if adds != 2 || dels != 1 {
		t.Fatalf("adds=%d dels=%d", adds, dels)
	}
}

func TestRenderBashLineEmptyCommand(t *testing.T) {
	out := renderShellCall(Message{Role: RoleTool, Text: "bash", Tool: "bash"})
	if out != "$" {
		t.Fatalf("empty bash: %q", out)
	}
}

func TestRenderShellCallFullCommand(t *testing.T) {
	cmd := "go test ./...\n-count=1"
	out := renderShellCall(Message{
		Role: RoleTool,
		Text: "bash " + cmd,
		Tool: "bash",
	})
	if !strings.HasPrefix(out, "$ "+cmd) {
		t.Fatalf("want full command: %q", out)
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
	// layout() keeps mainH + gapH constant so the input row does not jump when
	// the gap grows from the reserved blank to busy status or a command overlay.
	const regionH = 20
	overlay := "a\nb\nc\nd\ne" // 5 lines
	input, footer := "INPUT", "FOOTER"
	status := lipgloss.JoinVertical(lipgloss.Left, "", "⠋ working", "")

	stack := func(gap string) string {
		gh := styles.GapBeforeInput
		if gap != "" {
			gh = lipgloss.Height(gap)
		}
		mainH := regionH - gh
		if mainH < 1 {
			mainH = 1
		}
		main := strings.Repeat("m\n", mainH-1) + "m"
		return stackMainChrome(main, gap, input, footer)
	}
	withGap := stack("")
	withStatus := stack(status)
	withOverlay := stack(overlay)

	// Input should sit at the same absolute row whether the gap is blank,
	// showing the busy spinner, or replaced by an overlay.
	gapIdx := strings.Index(withGap, "INPUT")
	statusIdx := strings.Index(withStatus, "INPUT")
	overlayIdx := strings.Index(withOverlay, "INPUT")
	if gapIdx < 0 || statusIdx < 0 || overlayIdx < 0 {
		t.Fatal("missing INPUT")
	}
	gapH := lipgloss.Height(withGap[:gapIdx])
	if h := lipgloss.Height(withStatus[:statusIdx]); h != gapH {
		t.Fatalf("input jumped with status: gap-stack h=%d status-stack h=%d", gapH, h)
	}
	if h := lipgloss.Height(withOverlay[:overlayIdx]); h != gapH {
		t.Fatalf("input jumped with overlay: gap-stack h=%d overlay-stack h=%d", gapH, h)
	}
}
