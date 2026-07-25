package tui

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

func TestRenderMarkdownHeadingsBoldCode(t *testing.T) {
	in := "# Title\n\nThis is **bold** and `code`.\n"
	out := stripANSI(renderMarkdown(in, 60))
	if !strings.Contains(out, "Title") {
		t.Fatalf("missing heading text: %q", out)
	}
	if strings.Contains(out, "# Title") || strings.HasPrefix(strings.TrimSpace(out), "#") {
		t.Fatalf("heading should not show hashes: %q", out)
	}
	if !strings.Contains(out, "bold") {
		t.Fatalf("missing bold text: %q", out)
	}
	if !strings.Contains(out, "code") {
		t.Fatalf("missing inline code: %q", out)
	}
	if strings.Contains(out, "**bold**") {
		t.Fatalf("bold not rendered: %q", out)
	}
	if strings.Contains(out, "`code`") {
		t.Fatalf("inline code not rendered: %q", out)
	}
}

func TestRenderMarkdownListAndTable(t *testing.T) {
	in := "- one\n- two\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n"
	out := stripANSI(renderMarkdown(in, 60))
	if !strings.Contains(out, "- ") {
		t.Fatalf("missing bullet marker: %q", out)
	}
	if !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Fatalf("missing list items: %q", out)
	}
	if !strings.Contains(out, "A") || !strings.Contains(out, "B") {
		t.Fatalf("missing table headers: %q", out)
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "2") {
		t.Fatalf("missing table cells: %q", out)
	}
}

func TestRenderMarkdownTasksAndOrdered(t *testing.T) {
	in := "- [x] done\n- [ ] todo\n\n1. first\n2. second\n"
	out := stripANSI(renderMarkdown(in, 60))
	if !strings.Contains(out, "[✓]") {
		t.Fatalf("missing ticked checkbox: %q", out)
	}
	if !strings.Contains(out, "[ ]") {
		t.Fatalf("missing unticked checkbox: %q", out)
	}
	if !strings.Contains(out, "1.") || !strings.Contains(out, "2.") {
		t.Fatalf("missing ordered markers: %q", out)
	}
}

func TestRenderMarkdownWidthWrap(t *testing.T) {
	in := strings.Repeat("word ", 40)
	narrow := stripANSI(renderMarkdown(in, 20))
	wide := stripANSI(renderMarkdown(in, 80))
	narrowLines := strings.Count(narrow, "\n")
	wideLines := strings.Count(wide, "\n")
	if narrowLines <= wideLines {
		t.Fatalf("expected narrow wrap more lines: narrow=%d wide=%d", narrowLines, wideLines)
	}
}

func TestMessageMarkdownCache(t *testing.T) {
	msg := &Message{Role: RoleAgent, Text: "## Cached\n\nhello **world**"}
	a := msg.cachedMarkdown(msg.Text, 50)
	if msg.md == "" || msg.mdSource != msg.Text || msg.mdWidth != 50 {
		t.Fatalf("cache not populated: md=%q src=%q w=%d", msg.md, msg.mdSource, msg.mdWidth)
	}
	msg.md = "SENTINEL"
	b := msg.cachedMarkdown(msg.Text, 50)
	if b != "SENTINEL" {
		t.Fatalf("expected cache hit, got %q", b)
	}
	_ = a

	msg.Text += "!"
	c := msg.cachedMarkdown(msg.Text, 50)
	if c == "SENTINEL" {
		t.Fatal("expected cache invalidate on Text change")
	}
	if msg.mdSource != msg.Text {
		t.Fatalf("cache source not updated: %q vs %q", msg.mdSource, msg.Text)
	}

	d := msg.cachedMarkdown(msg.Text, 40)
	if d == c && msg.mdWidth != 40 {
		t.Fatal("expected re-render on width change")
	}
	if msg.mdWidth != 40 {
		t.Fatalf("width not updated: %d", msg.mdWidth)
	}
}

func TestStreamSplit(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		settled string
		tail    string
	}{
		{name: "empty", in: "", settled: "", tail: ""},
		{name: "growing paragraph", in: "Here is some", settled: "", tail: "Here is some"},
		{name: "closed heading", in: "## Summary\n\n", settled: "## Summary\n\n", tail: ""},
		{name: "heading plus tail", in: "## Summary\n\nHere is ", settled: "## Summary", tail: "Here is "},
		{name: "open fence", in: "Done.\n\n```go\nfunc main() {\n", settled: "Done.", tail: "```go\nfunc main() {\n"},
		{name: "table stays tail", in: "| A | B |\n| --- | --- |\n| 1 |", settled: "", tail: "| A | B |\n| --- | --- |\n| 1 |"},
		// ``` does not close ~~~ (marker-aware, not parity).
		{name: "mismatched markers stay open", in: "~~~\na\n```\nb\n", settled: "", tail: "~~~\na\n```\nb\n"},
		// Info string never closes (CommonMark).
		{name: "labeled line does not close", in: "```\na\n```go\nb\n", settled: "", tail: "```\na\n```go\nb\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			settled, tail := streamSplit(tc.in)
			if settled != tc.settled || tail != tc.tail {
				t.Fatalf("streamSplit(%q) = %q, %q; want %q, %q", tc.in, settled, tail, tc.settled, tc.tail)
			}
		})
	}
}

func TestStreamingMarkdown(t *testing.T) {
	msg := &Message{Role: RoleAgent, Text: "## Summary\n\nHere is "}
	out := stripANSI(msg.streamingMarkdown(msg.Text, 60))
	if strings.Contains(out, "##") {
		t.Fatalf("heading should be styled without hashes: %q", out)
	}
	if !strings.Contains(out, "Summary") {
		t.Fatalf("missing heading text: %q", out)
	}
	if !strings.Contains(out, "Here is ") {
		t.Fatalf("missing plain tail: %q", out)
	}

	open := &Message{Role: RoleAgent, Text: "Intro.\n\n```go\nfunc main() {\n"}
	openOut := stripANSI(open.streamingMarkdown(open.Text, 60))
	if !strings.Contains(openOut, "Intro.") {
		t.Fatalf("missing settled prose: %q", openOut)
	}
	if !strings.Contains(openOut, "```go") {
		t.Fatalf("open fence should stay plain: %q", openOut)
	}
}

func TestStreamingMarkdownCache(t *testing.T) {
	msg := &Message{Role: RoleAgent, Text: "## Done\n\npartial"}
	_ = msg.streamingMarkdown(msg.Text, 50)
	if msg.mdSource != "## Done" {
		t.Fatalf("cache should key settled prefix, got %q", msg.mdSource)
	}
	msg.md = "SENTINEL"
	msg.Text = "## Done\n\npartial more"
	out := msg.streamingMarkdown(msg.Text, 50)
	if !strings.Contains(out, "SENTINEL") {
		t.Fatalf("tail growth should hit settled cache: %q", out)
	}
	msg.Text = "## Done\n\nnext\n\npartial"
	_ = msg.streamingMarkdown(msg.Text, 50)
	if msg.md == "SENTINEL" {
		t.Fatal("new settled boundary should invalidate cache")
	}
}

func TestMessageRenderStreamingPlain(t *testing.T) {
	msg := &Message{Role: RoleAgent, Text: "```go\nfunc main() {\n"}
	plain := stripANSI(msg.streamingMarkdown(msg.Text, 40))
	if !strings.Contains(plain, "```go") {
		t.Fatalf("streaming should keep raw fence: %q", plain)
	}

	closed := &Message{Role: RoleAgent, Text: "```go\nfunc main() {}\n```"}
	rendered := stripANSI(closed.render(40, 0, lipgloss.NewStyle(), false))
	if strings.Contains(rendered, "```go") {
		t.Fatalf("finalized fenced block should not show fence markers: %q", rendered)
	}
	if !strings.Contains(rendered, "func main()") {
		t.Fatalf("missing code body: %q", rendered)
	}
}

func TestPlainAgentMatchesMarkdownWrap(t *testing.T) {
	// Plain prose: lipgloss Width and glamour WordWrap should agree at margin 0.
	// Ignore lipgloss trailing pad — only break positions matter.
	in := strings.Repeat("word ", 30)
	plain := trimRightLines(stripANSI(plainAgent(in, 40)))
	md := trimRightLines(stripANSI(renderMarkdown(in, 40)))
	if plain != md {
		t.Fatalf("wrap mismatch:\nplain=%q\nmd=%q", plain, md)
	}
}

func trimRightLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

func TestRenderMarkdownCodeBlockUsesANSI(t *testing.T) {
	in := "```go\nfunc main() { println(\"hi\") }\n```\n"
	out := renderMarkdown(in, 60)
	if !strings.Contains(stripANSI(out), "func main()") {
		t.Fatalf("missing code body: %q", stripANSI(out))
	}
	if strings.Contains(out, "38;5;") || strings.Contains(out, "38;2;") {
		t.Fatalf("expected terminal16 (no 256/truecolor), got: %q", out)
	}
}

func TestRenderMarkdownUnlabeledFenceDefaultFG(t *testing.T) {
	// Unlabeled fences must not use chroma Error tint or forced native Text→white.
	in := "```\ncmd/zeta\n  ├── internal/tools\n  └── internal/paths\n```\n"
	out := renderMarkdown(in, 80)
	if strings.Contains(out, "\x1b[31m\x1b[47m") {
		t.Fatalf("unlabeled fence should not use Error style: %q", out)
	}
	if strings.Contains(out, "\x1b[37m") {
		t.Fatalf("unlabeled fence should use default fg, not ANSI white: %q", out)
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "internal/tools") || !strings.Contains(plain, "├──") {
		t.Fatalf("missing diagram text: %q", plain)
	}
}

func TestRenderMarkdownFenceSpacing(t *testing.T) {
	bare := trimRightLines(stripANSI(renderMarkdown("before\n\n```\ndiagram\n```\n\nafter\n", 40)))
	goBlock := trimRightLines(stripANSI(renderMarkdown("before\n\n```go\nx\n```\n\nafter\n", 40)))
	for name, out := range map[string]string{"bare": bare, "go": goBlock} {
		if !strings.Contains(out, "before\n\n") || !strings.Contains(out, "\n\nafter") {
			t.Fatalf("%s missing vertical spacing: %q", name, out)
		}
	}
}

func TestRenderMarkdownEmpty(t *testing.T) {
	if got := renderMarkdown("", 40); got != "" {
		t.Fatalf("empty input: got %q", got)
	}
}
