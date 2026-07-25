package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/styles"
)

func TestExtractSelectionSingleLine(t *testing.T) {
	content := "hello world"
	got := extractSelectionString(content, selPos{0, 6}, selPos{0, 10})
	if got != "world" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractSelectionMultiLine(t *testing.T) {
	content := "aaa\nbbb\nccc"
	got := extractSelectionString(content, selPos{0, 1}, selPos{2, 1})
	want := "aa\nbbb\ncc"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExtractSelectionStripsANSI(t *testing.T) {
	content := "\x1b[32mgreen\x1b[0m text"
	got := extractSelectionString(content, selPos{0, 0}, selPos{0, 4})
	if got != "green" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractSelectionNoScrollbarChars(t *testing.T) {
	content := "line one\nline two"
	got := extractSelectionString(content, selPos{0, 0}, selPos{1, 7})
	if strings.ContainsAny(got, "│█") {
		t.Fatalf("scrollbar leaked: %q", got)
	}
	if got != "line one\nline two" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractSelectionWrappedDisplayLines(t *testing.T) {
	long := strings.Repeat("abcd", 20) // 80 cells
	lines := wrapContentLines(long, 40)
	if len(lines) != 2 {
		t.Fatalf("wrap lines=%d want 2", len(lines))
	}
	got := extractSelection(lines, selPos{0, 0}, selPos{1, 39})
	if got != long[:40]+"\n"+long[40:] {
		t.Fatalf("got %q", got)
	}
}

func TestHighlightSelection(t *testing.T) {
	frame := "abcdef\nghijkl"
	out := highlightSelection(frame, 0, selPos{0, 2}, selPos{0, 4})
	plain := stripANSI(out)
	if !strings.Contains(plain, "abcdef") {
		t.Fatalf("lost text: %q", plain)
	}
	if out == frame {
		t.Fatal("expected highlight styling")
	}
}

func TestTranscriptPosRejectsScrollbar(t *testing.T) {
	m := Model{
		ready:         true,
		width:         40,
		contentW:      37,
		showScrollbar: true,
		messages:      []Message{{Role: RoleUser, Text: "hi"}},
		viewport:      viewport.New(),
	}
	m.viewport.SetWidth(m.contentW)
	m.viewport.SetHeight(10)
	m.viewport.SetContent(strings.Repeat("x", 50) + "\n" + strings.Repeat("y\n", 20))

	if _, ok := m.transcriptPos(39, 0, false); ok {
		t.Fatal("scrollbar column should miss")
	}
	p, ok := m.transcriptPos(styles.ContentInset, 0, false)
	if !ok {
		t.Fatal("content should hit")
	}
	if p.col != 0 || p.line != 0 {
		t.Fatalf("pos %+v", p)
	}
}

func TestTranscriptPosAppliesYOffset(t *testing.T) {
	m := Model{
		ready:    true,
		width:    40,
		contentW: 38,
		messages: []Message{{Role: RoleUser, Text: "hi"}},
		viewport: viewport.New(),
	}
	m.viewport.SetWidth(m.contentW)
	m.viewport.SetHeight(5)
	m.viewport.SetContent(strings.Repeat("line\n", 30))
	m.viewport.SetYOffset(10)

	p, ok := m.transcriptPos(styles.ContentInset, 2, false)
	if !ok {
		t.Fatal("expected hit")
	}
	if p.line != 12 {
		t.Fatalf("line=%d want 12", p.line)
	}
}

func TestTranscriptPosClamp(t *testing.T) {
	m := Model{
		ready:         true,
		width:         40,
		contentW:      37,
		showScrollbar: true,
		messages:      []Message{{Role: RoleUser, Text: "hi"}},
		viewport:      viewport.New(),
	}
	m.viewport.SetWidth(m.contentW)
	m.viewport.SetHeight(10)
	m.viewport.SetContent("hello")

	p, ok := m.transcriptPos(39, 0, true) // scrollbar col → clamp to last content cell
	if !ok {
		t.Fatal("clamp should hit")
	}
	if p.col != 36 {
		t.Fatalf("col=%d want 36", p.col)
	}
}

func TestSelectionDragThreshold(t *testing.T) {
	m := Model{
		ready:    true,
		width:    42,
		height:   20,
		contentW: 40,
		messages: []Message{{Role: RoleUser, Text: "hi"}},
		viewport: viewport.New(),
	}
	m.viewport.SetWidth(40)
	m.viewport.SetHeight(10)
	m.viewport.SetContent("abcdef")

	m.sel.start(selPos{0, 0})
	m.sel.dragTo(selPos{0, 1}) // distance 1 — at threshold, not past
	if m.finishSelectionDrag() != nil {
		t.Fatal("distance <= threshold should not copy")
	}
	if m.sel.has() {
		t.Fatal("should clear after tiny drag")
	}

	m.sel.start(selPos{0, 0})
	m.sel.dragTo(selPos{0, 2}) // distance 2
	if m.finishSelectionDrag() == nil {
		t.Fatal("distance > threshold should copy")
	}
	if m.sel.has() {
		t.Fatal("should clear after copy")
	}
}

func TestSelectionNormalized(t *testing.T) {
	var s transcriptSel
	s.start(selPos{2, 5})
	s.dragTo(selPos{1, 0})
	a, b := s.normalized()
	if a != (selPos{1, 0}) || b != (selPos{2, 5}) {
		t.Fatalf("got %+v %+v", a, b)
	}
}

func TestSelectionReleaseExtendsWithoutMotion(t *testing.T) {
	m := Model{
		ready:    true,
		width:    42,
		height:   20,
		contentW: 40,
		messages: []Message{{Role: RoleUser, Text: "hi"}},
		viewport: viewport.New(),
	}
	m.viewport.SoftWrap = true
	m.viewport.SetWidth(40)
	m.viewport.SetHeight(10)
	m.viewport.SetContent("l0\nl1\nl2\nl3\nl4")

	if _, ok := m.handleSelectionMouse(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft}); !ok {
		t.Fatal("expected click handled")
	}
	if p, ok := m.transcriptPos(5, 3, true); ok {
		m.sel.dragTo(p)
	}
	got := m.selectedText()
	cmd, ok := m.handleSelectionMouse(tea.MouseReleaseMsg{X: 5, Y: 3, Button: tea.MouseLeft})

	if strings.Count(got, "\n") != 3 {
		t.Fatalf("want 4 lines before finish, got %q", got)
	}
	if !ok || cmd == nil {
		t.Fatal("expected copy cmd on release")
	}
	if m.sel.has() {
		t.Fatal("selection should clear after copy-on-up")
	}
}

func TestSelectionBlurFinishesDrag(t *testing.T) {
	m := Model{
		ready:    true,
		width:    42,
		height:   20,
		contentW: 40,
		messages: []Message{{Role: RoleUser, Text: "hi"}},
		viewport: viewport.New(),
	}
	m.viewport.SetWidth(40)
	m.viewport.SetHeight(10)
	m.viewport.SetContent("a\nb\nc\nd")

	if _, ok := m.handleSelectionMouse(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft}); !ok {
		t.Fatal("expected click handled")
	}
	m.sel.dragTo(selPos{2, 0})
	if !m.sel.dragging {
		t.Fatal("expected dragging")
	}
	cmd := m.finishSelectionDrag()
	if m.sel.dragging {
		t.Fatal("finish should end drag")
	}
	if cmd == nil {
		t.Fatal("blur mid-drag should copy")
	}
	if m.sel.has() {
		t.Fatal("selection should clear after copy")
	}
}

func TestOutsideTerminal(t *testing.T) {
	m := Model{width: 80, height: 24}
	if m.outsideTerminal(0, 0) || m.outsideTerminal(79, 23) {
		t.Fatal("in-bounds should be inside")
	}
	if !m.outsideTerminal(-1, 0) || !m.outsideTerminal(80, 0) || !m.outsideTerminal(0, 24) {
		t.Fatal("out-of-bounds should be outside")
	}
}

func TestModelSelectedTextMultiLine(t *testing.T) {
	m := Model{
		ready:    true,
		width:    42,
		contentW: 40,
		messages: []Message{{Role: RoleUser, Text: "hi"}},
		viewport: viewport.New(),
	}
	m.viewport.SetWidth(40)
	m.viewport.SetHeight(10)
	m.viewport.SetContent("alpha\nbeta\ngamma")
	m.sel.start(selPos{0, 0})
	m.sel.dragTo(selPos{2, 4})
	got := m.selectedText()
	if got != "alpha\nbeta\ngamma" {
		t.Fatalf("got %q", got)
	}
}

func TestCopiedFlashLine(t *testing.T) {
	if !strings.Contains(stripANSI(copiedFlashLine()), "Copied") {
		t.Fatal("missing Copied")
	}
}
