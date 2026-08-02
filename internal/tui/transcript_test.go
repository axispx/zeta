package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestLiveFromIdleAndStreaming(t *testing.T) {
	m := testModel()
	m.messages = []Message{
		{Role: RoleUser, Text: "hi"},
		{Role: RoleAgent, Text: "hello"},
	}
	if got := m.liveFrom(); got != 2 {
		t.Fatalf("idle liveFrom=%d, want 2", got)
	}

	m.turn = &turnSession{streaming: true, activeTool: -1}
	if got := m.liveFrom(); got != 1 {
		t.Fatalf("streaming agent liveFrom=%d, want 1", got)
	}

	m.turn.streaming = false
	m.turn.thinking = "hmm"
	if got := m.liveFrom(); got != 2 {
		t.Fatalf("thinking-only liveFrom=%d, want 2", got)
	}
}

func TestLiveFromActiveToolRun(t *testing.T) {
	m := testModel()
	m.messages = []Message{
		{Role: RoleUser, Text: "run"},
		{Role: RoleTool, Text: "read a", Tool: "read"},
		{Role: RoleTool, Text: "bash ls", Tool: "bash", Out: "x"},
	}
	m.turn = &turnSession{activeTool: 2, streaming: false}
	if got := m.liveFrom(); got != 1 {
		t.Fatalf("tool run liveFrom=%d, want 1 (run start)", got)
	}
}

func TestTranscriptCacheTailOnlyOnStreamDelta(t *testing.T) {
	m := testModel()
	m.width = 80
	m.height = 40
	m.contentW = 60
	m.messages = []Message{
		{Role: RoleUser, Text: "prompt"},
		{Role: RoleAgent, Text: "## Settled\n\nold body"},
	}
	m.layout()
	m.setTranscriptContent()
	if m.tx.frozen != 2 {
		t.Fatalf("after idle freeze frozen=%d, want 2", m.tx.frozen)
	}
	prefix := m.tx.prefix
	if prefix == "" {
		t.Fatal("expected non-empty prefix")
	}

	// Stream a new agent turn: settled history must stay in prefix.
	m.messages = append(m.messages, Message{Role: RoleUser, Text: "again"})
	m.setTranscriptContent()
	m.turn = &turnSession{streaming: true, activeTool: -1}
	m.messages = append(m.messages, Message{Role: RoleAgent, Text: "He"})
	m.setTranscriptContent()
	if m.tx.frozen != 3 {
		t.Fatalf("streaming frozen=%d, want 3 (before live agent)", m.tx.frozen)
	}
	if !strings.Contains(m.tx.prefix, "prompt") || !strings.Contains(m.tx.prefix, "Settled") {
		t.Fatalf("prefix lost settled history: %q", stripANSI(m.tx.prefix))
	}
	prevPrefix := m.tx.prefix

	m.messages[len(m.messages)-1].Text += "llo"
	m.setTranscriptContent()
	if m.tx.prefix != prevPrefix {
		t.Fatal("stream delta rebuilt prefix; expected tail-only refresh")
	}
	if m.tx.frozen != 3 {
		t.Fatalf("after delta frozen=%d, want 3", m.tx.frozen)
	}
	got := m.viewport.GetContent()
	if !strings.Contains(stripANSI(got), "Hello") {
		t.Fatalf("missing streamed text: %q", stripANSI(got))
	}
}

func TestTranscriptCacheMatchesFullRender(t *testing.T) {
	m := testModel()
	m.width = 80
	m.height = 40
	m.contentW = 56
	m.messages = []Message{
		{Role: RoleUser, Text: "do stuff"},
		{Role: RoleAgent, Text: "plan:\n\n1. one\n2. two"},
		{Role: RoleTool, Text: "read f", Tool: "read"},
		{Role: RoleTool, Text: "bash echo hi", Tool: "bash", Out: "hi\n"},
		{Role: RoleAgent, Text: "done **ok**"},
	}
	m.turn = &turnSession{streaming: true, activeTool: -1}
	// Last agent is live; force incremental path from empty cache.
	m.tx.invalidate()
	m.setTranscriptContent()
	inc := m.viewport.GetContent()

	m.tx.invalidate()
	full := m.buildTranscriptFull()
	if inc != full {
		t.Fatalf("incremental != full\ninc:\n%s\nfull:\n%s", stripANSI(inc), stripANSI(full))
	}
}

func TestTranscriptCacheToolRunRewind(t *testing.T) {
	m := testModel()
	m.width = 80
	m.height = 40
	m.contentW = 60
	m.messages = []Message{
		{Role: RoleUser, Text: "x"},
		{Role: RoleTool, Text: "read a", Tool: "read"},
	}
	// Tool completes → freeze including tool.
	m.turn = &turnSession{activeTool: -1}
	m.setTranscriptContent()
	if m.tx.frozen != 2 {
		t.Fatalf("frozen=%d, want 2", m.tx.frozen)
	}

	// Second tool extends the run backward into frozen region.
	m.messages = append(m.messages, Message{Role: RoleTool, Text: "bash ls", Tool: "bash"})
	m.turn.activeTool = 2
	m.setTranscriptContent()
	if m.tx.frozen != 1 {
		t.Fatalf("after tool run extend frozen=%d, want 1", m.tx.frozen)
	}
	got := stripANSI(m.viewport.GetContent())
	if !strings.Contains(got, "read") {
		t.Fatalf("tool run missing read row: %q", got)
	}
	full := m.buildTranscriptFull()
	if m.viewport.GetContent() != full {
		t.Fatalf("rewind path != full render")
	}
}

func TestTranscriptCacheThinkingOnly(t *testing.T) {
	m := testModel()
	m.width = 80
	m.height = 40
	m.contentW = 60
	m.messages = []Message{{Role: RoleUser, Text: "think"}}
	m.turn = &turnSession{activeTool: -1, thinking: "step one"}
	m.setTranscriptContent()
	prefix := m.tx.prefix
	if m.tx.frozen != 1 {
		t.Fatalf("frozen=%d, want 1", m.tx.frozen)
	}
	m.turn.thinking = "step one\nstep two"
	m.setTranscriptContent()
	if m.tx.prefix != prefix {
		t.Fatal("thinking update rebuilt message prefix")
	}
	if !strings.Contains(stripANSI(m.viewport.GetContent()), "step two") {
		t.Fatalf("missing thinking tail: %q", stripANSI(m.viewport.GetContent()))
	}
}

func TestTranscriptCacheWidthInvalidates(t *testing.T) {
	m := testModel()
	m.contentW = 40
	m.messages = []Message{{Role: RoleUser, Text: "wide enough to wrap maybe"}}
	m.setTranscriptContent()
	if m.tx.width != 40 || m.tx.frozen != 1 {
		t.Fatalf("tx=%+v", m.tx)
	}
	m.contentW = 20
	m.setTranscriptContent()
	if m.tx.width != 20 {
		t.Fatalf("width not updated: %d", m.tx.width)
	}
	// Full rebuild still freezes idle content.
	if m.tx.frozen != 1 {
		t.Fatalf("frozen=%d, want 1", m.tx.frozen)
	}
}

func TestTranscriptCacheManyDeltasStablePrefix(t *testing.T) {
	m := testModel()
	m.width = 100
	m.height = 50
	m.contentW = 80
	var hist []string
	for i := 0; i < 20; i++ {
		hist = append(hist, fmt.Sprintf("history block %02d with some text", i))
	}
	m.messages = []Message{
		{Role: RoleAgent, Text: strings.Join(hist, "\n\n")},
		{Role: RoleUser, Text: "go"},
	}
	m.setTranscriptContent()
	baseFrozen := m.tx.frozen
	basePrefix := m.tx.prefix

	m.turn = &turnSession{streaming: true, activeTool: -1}
	m.messages = append(m.messages, Message{Role: RoleAgent, Text: ""})
	for i := 0; i < 50; i++ {
		m.messages[len(m.messages)-1].Text += "x"
		m.setTranscriptContent()
		if m.tx.prefix != basePrefix {
			t.Fatalf("delta %d rebuilt prefix", i)
		}
		if m.tx.frozen != baseFrozen {
			t.Fatalf("delta %d frozen=%d, want %d", i, m.tx.frozen, baseFrozen)
		}
	}
	if !strings.Contains(m.viewport.GetContent(), strings.Repeat("x", 50)) {
		t.Fatal("missing streamed xs")
	}
}

// overflowViewport seeds a viewport tall enough that top/mid/bottom are distinct.
func overflowViewport(t *testing.T) Model {
	t.Helper()
	m := testModel()
	m.width = 80
	m.height = 24
	m.mainCache = &mainViewCache{}
	m.viewport.SoftWrap = true
	m.viewport.SetWidth(40)
	m.viewport.SetHeight(8)
	// Raw content — bypass message markdown so line count is predictable.
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "history line %02d\n", i)
	}
	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
	if m.viewport.TotalLineCount() <= m.viewport.Height() {
		t.Fatalf("need overflow, total=%d height=%d", m.viewport.TotalLineCount(), m.viewport.Height())
	}
	return m
}

func TestRejectEdgeScroll(t *testing.T) {
	m := overflowViewport(t)
	if !m.viewport.AtBottom() {
		t.Fatal("expected initial stick-to-bottom")
	}

	// At bottom: down is dead, up moves.
	if !m.rejectEdgeScroll(tea.MouseWheelMsg{Button: tea.MouseWheelDown}) {
		t.Fatal("expected reject wheel-down at bottom")
	}
	if m.rejectEdgeScroll(tea.MouseWheelMsg{Button: tea.MouseWheelUp}) {
		t.Fatal("did not expect reject wheel-up away from top")
	}

	m.viewport.GotoTop()
	if !m.viewport.AtTop() {
		t.Fatal("expected at top")
	}
	if !m.rejectEdgeScroll(tea.MouseWheelMsg{Button: tea.MouseWheelUp}) {
		t.Fatal("expected reject wheel-up at top")
	}
	if m.rejectEdgeScroll(tea.MouseWheelMsg{Button: tea.MouseWheelDown}) {
		t.Fatal("did not expect reject wheel-down away from bottom")
	}

	// Mid-scroll: neither edge rejects.
	m.viewport.ScrollDown(4)
	if m.viewport.AtTop() || m.viewport.AtBottom() {
		t.Fatalf("expected mid-scroll position, y=%d top=%v bot=%v",
			m.viewport.YOffset(), m.viewport.AtTop(), m.viewport.AtBottom())
	}
	if m.rejectEdgeScroll(tea.MouseWheelMsg{Button: tea.MouseWheelUp}) ||
		m.rejectEdgeScroll(tea.MouseWheelMsg{Button: tea.MouseWheelDown}) {
		t.Fatal("mid-scroll should accept both wheel directions")
	}
}

func TestRejectEdgeScrollUpdateNoMove(t *testing.T) {
	m := overflowViewport(t)
	m.viewport.GotoTop()
	// Non-empty messages so mainView takes the viewport path (not the banner).
	m.messages = []Message{{Role: RoleSystem, Text: "x"}}
	off := m.viewport.YOffset()

	next, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m = next.(Model)
	if got := m.viewport.YOffset(); got != off {
		t.Fatalf("YOffset=%d after rejected wheel-up, want %d", got, off)
	}
}

func TestMainViewCacheHitOnSameOffset(t *testing.T) {
	m := overflowViewport(t)
	m.messages = []Message{{Role: RoleSystem, Text: "x"}}
	m.showScrollbar = true
	m.viewport.GotoTop()

	first := m.mainView()
	if m.mainCache.text == "" {
		t.Fatal("expected main view cache fill")
	}
	second := m.mainView()
	if first != second {
		t.Fatal("cache miss on unchanged offset")
	}
	if m.mainCache.text != first {
		t.Fatal("cache text drifted")
	}

	// Content change must bust the cache even at the same YOffset.
	m.viewport.SetContent("VISIBLE_MARKER_A\n" + strings.Repeat("line\n", 30))
	m.invalidateMainView()
	if m.mainCache.text != "" {
		t.Fatal("invalidateMainView should clear cache text")
	}
	m.viewport.GotoTop()
	third := m.mainView()
	if !strings.Contains(third, "VISIBLE_MARKER_A") {
		t.Fatalf("expected new content in paint, got %q", stripANSI(third))
	}
	if third == first {
		t.Fatal("expected different paint after content change")
	}
}

// Submitting a new message jumps to the bottom even when scrolled up.
func TestSubmitScrollsToBottomWhenScrolledUp(t *testing.T) {
	m := testModelWithClient()
	m.width = 80
	m.height = 24

	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("history line %02d", i))
	}
	m.messages = []Message{
		{Role: RoleAgent, Text: strings.Join(lines, "\n")},
	}
	m.repaintTranscript()
	if !m.viewport.AtBottom() {
		t.Fatal("expected initial stick-to-bottom")
	}

	m.viewport.ScrollUp(10)
	if m.viewport.AtBottom() {
		t.Fatal("expected scrolled away from bottom")
	}

	// submit starts a turn cmd against an unroutable client — do not run it.
	_ = m.submit("new prompt", nil)
	if !m.viewport.AtBottom() {
		t.Fatal("expected submit to scroll to bottom")
	}
	if got := m.messages[len(m.messages)-1]; got.Role != RoleUser || got.Text != "new prompt" {
		t.Fatalf("last message = %+v", got)
	}
}

// Stream paints must not yank scroll position after the user leaves the bottom.
func TestStreamPaintPreservesScrollWhenNotAtBottom(t *testing.T) {
	m := testModel()
	m.width = 80
	m.height = 20
	m.contentW = 60
	m.viewport.SetWidth(60)
	m.viewport.SetHeight(8)

	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("history line %02d", i))
	}
	m.messages = []Message{
		{Role: RoleAgent, Text: strings.Join(lines, "\n")},
		{Role: RoleUser, Text: "go"},
	}
	m.setTranscriptContent()
	if !m.viewport.AtBottom() {
		t.Fatal("expected initial stick-to-bottom")
	}

	m.viewport.ScrollUp(6)
	if m.viewport.AtBottom() {
		t.Fatal("expected scrolled away from bottom")
	}
	wantOff := m.viewport.YOffset()

	m.turn = &turnSession{streaming: true, activeTool: -1}
	m.messages = append(m.messages, Message{Role: RoleAgent, Text: ""})
	for i := 0; i < 20; i++ {
		m.messages[len(m.messages)-1].Text += "streamed token "
		m.setTranscriptContent()
		if m.viewport.AtBottom() {
			t.Fatalf("delta %d yanked to bottom", i)
		}
		if got := m.viewport.YOffset(); got != wantOff {
			t.Fatalf("delta %d YOffset=%d, want %d", i, got, wantOff)
		}
	}

	// Returning to the bottom resumes follow-on-stream.
	m.viewport.GotoBottom()
	m.messages[len(m.messages)-1].Text += "\n" + strings.Repeat("more\n", 10)
	m.setTranscriptContent()
	if !m.viewport.AtBottom() {
		t.Fatal("expected stick-to-bottom after user returns to bottom")
	}
}

// After scroll up then back down, stream paints should keep following the bottom.
func TestRepaintTranscriptResumesStickAfterScroll(t *testing.T) {
	m := testModel()
	m.width = 80
	m.height = 24

	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("history line %02d", i))
	}
	m.messages = []Message{
		{Role: RoleAgent, Text: strings.Join(lines, "\n")},
		{Role: RoleUser, Text: "go"},
	}
	m.repaintTranscript()
	if !m.showScrollbar {
		t.Fatal("expected overflow scrollbar")
	}
	if !m.viewport.AtBottom() {
		t.Fatal("expected initial stick-to-bottom")
	}

	m.viewport.ScrollUp(8)
	if m.viewport.AtBottom() {
		t.Fatal("expected scrolled away from bottom")
	}
	wantOff := m.viewport.YOffset()

	m.turn = &turnSession{streaming: true, activeTool: -1}
	m.messages = append(m.messages, Message{Role: RoleAgent, Text: ""})
	for i := 0; i < 15; i++ {
		m.messages[len(m.messages)-1].Text += "streamed token "
		m.repaintTranscript()
		if m.viewport.AtBottom() {
			t.Fatalf("delta %d yanked to bottom", i)
		}
		if got := m.viewport.YOffset(); got != wantOff {
			t.Fatalf("delta %d YOffset=%d, want %d", i, got, wantOff)
		}
		if !m.showScrollbar {
			t.Fatalf("delta %d dropped scrollbar", i)
		}
	}

	// Scroll back to bottom — subsequent paints must keep following.
	m.viewport.GotoBottom()
	for i := 0; i < 15; i++ {
		m.messages[len(m.messages)-1].Text += "\nmore line"
		m.repaintTranscript()
		if !m.viewport.AtBottom() {
			t.Fatalf("delta %d lost stick-to-bottom after return", i)
		}
	}
}
