package tui

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
)

func histModel(prompts ...string) Model {
	ta := textarea.New()
	ta.Focus()
	m := Model{textarea: ta}
	for _, p := range prompts {
		m.history = append(m.history, ai.Message{Role: ai.RoleUser, Text: p})
	}
	m.resetPromptHistory()
	return m
}

func histKey(s string) tea.KeyPressMsg {
	switch s {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	case "ctrl+n":
		return tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	default:
		return tea.KeyPressMsg{Text: s}
	}
}

func TestStepUser(t *testing.T) {
	h := []ai.Message{
		{Role: ai.RoleUser, Text: "one"},
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "two"},
	}
	if got := stepUser(h, len(h), -1); got != 2 {
		t.Fatalf("oldest-from-live = %d", got)
	}
	if got := stepUser(h, 2, -1); got != 0 {
		t.Fatalf("older-from-two = %d", got)
	}
	if got := stepUser(h, 0, -1); got != -1 {
		t.Fatalf("older-at-oldest = %d", got)
	}
	if got := stepUser(h, 0, +1); got != 2 {
		t.Fatalf("newer-from-one = %d", got)
	}
	if got := stepUser(h, 2, +1); got != -1 {
		t.Fatalf("newer-at-newest = %d", got)
	}
}

func TestPromptHistoryEmpty(t *testing.T) {
	m := histModel()
	if m.handlePromptHistoryKey(histKey("up")) {
		t.Fatal("up with empty history should pass through")
	}
	if m.handlePromptHistoryKey(histKey("down")) {
		t.Fatal("down with empty history should pass through")
	}
}

func TestPromptHistoryNavigate(t *testing.T) {
	m := histModel("first", "second")
	// Interleave assistant so indices are not dense user-only.
	m.history = []ai.Message{
		{Role: ai.RoleUser, Text: "first"},
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "second"},
	}
	m.resetPromptHistory()
	m.textarea.SetValue("draft")

	if !m.handlePromptHistoryKey(histKey("up")) {
		t.Fatal("up should be consumed")
	}
	if m.textarea.Value() != "second" {
		t.Fatalf("want second, got %q", m.textarea.Value())
	}
	if m.promptHist.draft != "draft" || m.promptHist.at != 2 {
		t.Fatalf("browse = %+v", m.promptHist)
	}

	if !m.handlePromptHistoryKey(histKey("ctrl+p")) {
		t.Fatal("ctrl+p should be consumed")
	}
	if m.textarea.Value() != "first" || m.promptHist.at != 0 {
		t.Fatalf("want first@0, got %q@%d", m.textarea.Value(), m.promptHist.at)
	}

	// Already at oldest: still consumed, value unchanged.
	if !m.handlePromptHistoryKey(histKey("up")) {
		t.Fatal("up at oldest should be consumed")
	}
	if m.textarea.Value() != "first" {
		t.Fatalf("still first, got %q", m.textarea.Value())
	}

	if !m.handlePromptHistoryKey(histKey("down")) {
		t.Fatal("down should be consumed")
	}
	if m.textarea.Value() != "second" {
		t.Fatalf("want second, got %q", m.textarea.Value())
	}

	if !m.handlePromptHistoryKey(histKey("ctrl+n")) {
		t.Fatal("ctrl+n should be consumed")
	}
	if m.textarea.Value() != "draft" {
		t.Fatalf("want draft restored, got %q", m.textarea.Value())
	}
	if !m.promptHist.live() || m.promptHist.draft != "" {
		t.Fatalf("want live, got %+v", m.promptHist)
	}
}

func TestPromptHistoryMultilineBoundary(t *testing.T) {
	m := histModel("old")
	m.textarea.SetValue("line1\nline2")
	m.textarea.MoveToEnd() // last line

	// From last line, up should NOT enter history (Line() > 0).
	if m.textarea.Line() == 0 {
		t.Fatal("expected cursor on last line of multiline")
	}
	if m.handlePromptHistoryKey(histKey("up")) {
		t.Fatal("up mid-draft should pass through to textarea")
	}
	if m.textarea.Value() != "line1\nline2" {
		t.Fatalf("value changed: %q", m.textarea.Value())
	}

	m.textarea.MoveToBegin()
	if m.textarea.Line() != 0 {
		t.Fatalf("Line = %d", m.textarea.Line())
	}
	if !m.handlePromptHistoryKey(histKey("up")) {
		t.Fatal("up at first line should enter history")
	}
	if m.textarea.Value() != "old" {
		t.Fatalf("want old, got %q", m.textarea.Value())
	}
	if m.promptHist.draft != "line1\nline2" {
		t.Fatalf("draft = %q", m.promptHist.draft)
	}

	// Down from first line of a multiline recall should pass through.
	m.textarea.SetValue("x\ny")
	m.textarea.MoveToBegin()
	if m.handlePromptHistoryKey(histKey("down")) {
		t.Fatal("down not on last line should pass through")
	}
}

func TestPromptHistoryEditExitsBrowse(t *testing.T) {
	m := histModel("old")
	if !m.handlePromptHistoryKey(histKey("up")) {
		t.Fatal("up")
	}
	before := m.textarea.Value()
	m.textarea.SetValue(before + "!")
	m.notePromptEdit(before)
	if !m.promptHist.live() {
		t.Fatalf("edit should exit browse, got %+v", m.promptHist)
	}
	if m.textarea.Value() != "old!" {
		t.Fatalf("value = %q", m.textarea.Value())
	}
	// Cursor-only change does not exit.
	_ = m.handlePromptHistoryKey(histKey("up"))
	before = m.textarea.Value()
	m.notePromptEdit(before)
	if m.promptHist.live() {
		t.Fatal("unchanged value should stay browsing")
	}
}

func TestResetPromptHistory(t *testing.T) {
	m := histModel("a")
	m.textarea.SetValue("x")
	_ = m.handlePromptHistoryKey(histKey("up"))
	m.resetInput()
	if !m.promptHist.live() || m.promptHist.draft != "" {
		t.Fatalf("prompts=%+v", m.promptHist)
	}
	if m.textarea.Value() != "" {
		t.Fatalf("input = %q", m.textarea.Value())
	}
}
