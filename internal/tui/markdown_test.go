package tui

import (
	"regexp"
	"strings"
	"testing"
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
	a := msg.agentMarkdown(50)
	if msg.md == "" || msg.mdSource != msg.Text || msg.mdWidth != 50 {
		t.Fatalf("cache not populated: md=%q src=%q w=%d", msg.md, msg.mdSource, msg.mdWidth)
	}
	msg.md = "SENTINEL"
	b := msg.agentMarkdown(50)
	if b != "SENTINEL" {
		t.Fatalf("expected cache hit, got %q", b)
	}
	_ = a

	msg.Text += "!"
	c := msg.agentMarkdown(50)
	if c == "SENTINEL" {
		t.Fatal("expected cache invalidate on Text change")
	}
	if msg.mdSource != msg.Text {
		t.Fatalf("cache source not updated: %q vs %q", msg.mdSource, msg.Text)
	}

	d := msg.agentMarkdown(40)
	if d == c && msg.mdWidth != 40 {
		t.Fatal("expected re-render on width change")
	}
	if msg.mdWidth != 40 {
		t.Fatalf("width not updated: %d", msg.mdWidth)
	}
}

func TestMessageRenderStreamingPlain(t *testing.T) {
	msg := &Message{Role: RoleAgent, Text: "```go\nfunc main() {\n"}
	plain := stripANSI(plainAgent(msg.Text, 40))
	if !strings.Contains(plain, "```go") {
		t.Fatalf("streaming should keep raw fence: %q", plain)
	}

	closed := &Message{Role: RoleAgent, Text: "```go\nfunc main() {}\n```"}
	rendered := stripANSI(closed.render(40, 0))
	if strings.Contains(rendered, "```go") {
		t.Fatalf("finalized fenced block should not show fence markers: %q", rendered)
	}
	if !strings.Contains(rendered, "func main()") {
		t.Fatalf("missing code body: %q", rendered)
	}
}

func TestPlainAgentMatchesMarkdownWrap(t *testing.T) {
	// Plain prose: lipgloss Width and glamour WordWrap should agree at margin 0.
	in := strings.Repeat("word ", 30)
	plain := stripANSI(plainAgent(in, 40))
	md := stripANSI(renderMarkdown(in, 40))
	if plain != md {
		t.Fatalf("wrap mismatch:\nplain=%q\nmd=%q", plain, md)
	}
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

func TestRenderMarkdownEmpty(t *testing.T) {
	if got := renderMarkdown("", 40); got != "" {
		t.Fatalf("empty input: got %q", got)
	}
}
