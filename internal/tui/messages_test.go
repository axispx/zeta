package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestToolRunAtGroupsTools(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Text: "hi"},
		{Role: RoleTool, Text: "read a.go"},
		{Role: RoleTool, Text: "read b.go"},
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
		{Role: RoleTool, Text: "read a.go"},
		{Role: RoleTool, Text: "read b.go"},
		{Role: RoleTool, Text: "read c.go"},
		{Role: RoleTool, Text: "read d.go"},
		{Role: RoleTool, Text: "edit e.go"},
	}
	out := renderToolGroup(msgs, 80, 0)
	if !strings.Contains(out, "read a.go") {
		t.Fatalf("missing first: %q", out)
	}
	if !strings.Contains(out, "read c.go") {
		t.Fatalf("missing third: %q", out)
	}
	if strings.Contains(out, "read d.go") {
		t.Fatalf("should hide 4th: %q", out)
	}
	if !strings.Contains(out, "+2 more") {
		t.Fatalf("missing overflow: %q", out)
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
	overlay := "a\nb\nc\nd\ne"                 // 5 lines
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
