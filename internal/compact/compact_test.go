package compact

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
)

func TestEstimateWithImages(t *testing.T) {
	plain := ai.Message{Role: ai.RoleUser, Text: "hi"}
	with := ai.Message{Role: ai.RoleUser, Text: "hi", Images: []ai.Image{{URL: "data:image/png;base64,AAAA", MIME: "image/png"}}}
	if Estimate([]ai.Message{with}) <= Estimate([]ai.Message{plain}) {
		t.Fatal("images should add token fudge")
	}
	if Estimate([]ai.Message{with}) < imageTokenFudge {
		t.Fatal("expected at least image fudge")
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf("empty: %d", got)
	}
	if got := EstimateTokens("ab"); got != 1 { // 2 runes → min 1
		t.Fatalf("short: %d", got)
	}
	if got := EstimateTokens(strings.Repeat("x", 40)); got != 10 {
		t.Fatalf("40 chars: %d", got)
	}
	// multibyte
	if got := EstimateTokens("你好世界"); got != 1 { // 4 runes
		t.Fatalf("unicode: %d", got)
	}
}

func TestNeeded(t *testing.T) {
	// Multi-turn so Select has a freeable head when over budget.
	old := ai.Message{Role: ai.RoleUser, Text: strings.Repeat("word ", 2000)}
	recent := ai.Message{Role: ai.RoleUser, Text: "recent"}
	hist := []ai.Message{old, recent}
	est := Estimate(hist)

	if Needed(nil, Config{ContextWindow: 1000}) {
		t.Fatal("empty history should not need compact")
	}
	if Needed(hist, Config{ContextWindow: 0}) {
		t.Fatal("zero window should not need compact")
	}
	if Needed(hist, Config{ContextWindow: est + DefaultBuffer + 100}) {
		t.Fatalf("under budget should not need compact (est=%d)", est)
	}
	if !Needed(hist, Config{ContextWindow: est + DefaultBuffer - 1, Keep: estimateMsg(recent) + 5}) {
		t.Fatalf("over budget with freeable head should need compact (est=%d)", est)
	}
	// overhead pushes over
	if !Needed(hist, Config{ContextWindow: est + DefaultBuffer + 50, Overhead: 100, Keep: estimateMsg(recent) + 5}) {
		t.Fatal("overhead should push over budget")
	}
	// Single oversized turn: over budget but nothing freeable.
	huge := []ai.Message{{Role: ai.RoleUser, Text: strings.Repeat("word ", 50_000)}}
	if Needed(huge, Config{ContextWindow: 8_000}) {
		t.Fatal("single oversized turn should not need compact")
	}
}

func TestIsCheckpointPrefix(t *testing.T) {
	cp := CheckpointMessage("sum")
	if !IsCheckpoint(cp) {
		t.Fatal("expected checkpoint")
	}
	// Tag only mid-message must not match.
	fake := ai.Message{Role: ai.RoleUser, Text: "see " + checkpointOpen + " later"}
	if IsCheckpoint(fake) {
		t.Fatal("mid-text tag should not be checkpoint")
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	sum := "## Task\n- ship compaction"
	m := CheckpointMessage(sum)
	if m.Role != ai.RoleUser {
		t.Fatalf("role: %s", m.Role)
	}
	if !IsCheckpoint(m) {
		t.Fatal("expected checkpoint")
	}
	got, ok := ParseSummary(m)
	if !ok || got != sum {
		t.Fatalf("parse: ok=%v got=%q", ok, got)
	}
	// empty summary still checkpoint
	m2 := CheckpointMessage("  ")
	if s, ok := ParseSummary(m2); !ok || s != "(no summary available)" {
		t.Fatalf("empty summary: %q ok=%v", s, ok)
	}
}

func TestSelectKeepsRecentTail(t *testing.T) {
	// Three messages with known sizes; keep only the last.
	mk := func(s string) ai.Message {
		return ai.Message{Role: ai.RoleUser, Text: s}
	}
	// Each ~25 tokens of text (+ framing)
	a := mk(strings.Repeat("aaaa ", 20))
	b := mk(strings.Repeat("bbbb ", 20))
	c := mk(strings.Repeat("cccc ", 20))
	hist := []ai.Message{a, b, c}

	// keep budget that fits only c
	keep := estimateMsg(c)
	sp := Select(hist, keep)
	if len(sp.Tail) != 1 || sp.Tail[0].Text != c.Text {
		t.Fatalf("tail=%v", msgsText(sp.Tail))
	}
	if len(sp.Head) != 2 {
		t.Fatalf("head len=%d want 2", len(sp.Head))
	}
}

func TestSelectPeelsPreviousCheckpoint(t *testing.T) {
	cp := CheckpointMessage("old summary")
	u := ai.Message{Role: ai.RoleUser, Text: "hello"}
	a := ai.Message{Role: ai.RoleAssistant, Text: "hi"}
	sp := Select([]ai.Message{cp, u, a}, 1_000_000)
	if sp.PreviousSummary != "old summary" {
		t.Fatalf("prev=%q", sp.PreviousSummary)
	}
	if len(sp.Head) != 0 {
		t.Fatalf("everything should fit tail, head=%d", len(sp.Head))
	}
	if len(sp.Tail) != 2 {
		t.Fatalf("tail=%d", len(sp.Tail))
	}
}

func TestSelectSnapsToUserTurn(t *testing.T) {
	// Budget would only fit the last assistant if we split by message — user-turn
	// snap must pull in the matching user message (and not the prior turn).
	oldU := ai.Message{Role: ai.RoleUser, Text: strings.Repeat("old ", 500)}
	oldA := ai.Message{
		Role: ai.RoleAssistant,
		Text: "calling",
		ToolCalls: []ai.ToolCall{
			{ID: "1", Name: "read", Arguments: `{"path":"a.go"}`},
		},
	}
	oldT := ai.Message{Role: ai.RoleTool, ToolCallID: "1", Text: "file contents here"}
	newU := ai.Message{Role: ai.RoleUser, Text: "recent question"}
	newA := ai.Message{Role: ai.RoleAssistant, Text: "recent answer"}

	keep := estimateMsg(newU) + estimateMsg(newA)
	sp := Select([]ai.Message{oldU, oldA, oldT, newU, newA}, keep)
	if len(sp.Tail) != 2 {
		t.Fatalf("tail len=%d roles=%v", len(sp.Tail), roles(sp.Tail))
	}
	if sp.Tail[0].Role != ai.RoleUser || sp.Tail[0].Text != newU.Text {
		t.Fatalf("tail should start at user turn: %+v", sp.Tail[0])
	}
	if sp.Tail[1].Text != newA.Text {
		t.Fatalf("tail assistant: %+v", sp.Tail[1])
	}
	if len(sp.Head) != 3 || sp.Head[0].Role != ai.RoleUser {
		t.Fatalf("head: %v", roles(sp.Head))
	}
}

func TestSelectKeepsOversizedNewestTurn(t *testing.T) {
	// One user turn larger than keep — still keep it whole (no mid-turn cut).
	u := ai.Message{Role: ai.RoleUser, Text: strings.Repeat("q ", 200)}
	a := ai.Message{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{
			{ID: "1", Name: "read", Arguments: `{}`},
		},
	}
	tool := ai.Message{Role: ai.RoleTool, ToolCallID: "1", Text: strings.Repeat("out ", 200)}
	keep := 10 // tiny
	sp := Select([]ai.Message{u, a, tool}, keep)
	if len(sp.Head) != 0 {
		t.Fatalf("head should be empty, got %v", roles(sp.Head))
	}
	if len(sp.Tail) != 3 || sp.Tail[0].Role != ai.RoleUser {
		t.Fatalf("tail=%v", roles(sp.Tail))
	}
}

func TestSelectNoUserMessages(t *testing.T) {
	a := ai.Message{Role: ai.RoleAssistant, Text: "orphan"}
	sp := Select([]ai.Message{a}, 100)
	if len(sp.Head) != 0 || len(sp.Tail) != 1 {
		t.Fatalf("head=%v tail=%v", roles(sp.Head), roles(sp.Tail))
	}
}

func TestBuildPromptIncludesPrevious(t *testing.T) {
	head := []ai.Message{{Role: ai.RoleUser, Text: "fix the bug"}}
	msgs := BuildPrompt("prior summary text", head)
	if len(msgs) != 2 {
		t.Fatalf("len=%d", len(msgs))
	}
	if msgs[0].Role != ai.RoleSystem {
		t.Fatalf("system role: %s", msgs[0].Role)
	}
	if !strings.Contains(msgs[1].Text, "<previous-summary>") {
		t.Fatal("missing previous-summary")
	}
	if !strings.Contains(msgs[1].Text, "prior summary text") {
		t.Fatal("missing previous body")
	}
	if !strings.Contains(msgs[1].Text, "[User]\nfix the bug") {
		t.Fatal("missing serialized head")
	}
	if !strings.Contains(msgs[1].Text, "## Task") {
		t.Fatal("missing template")
	}
}

func TestBuildPromptFresh(t *testing.T) {
	msgs := BuildPrompt("", []ai.Message{{Role: ai.RoleUser, Text: "hi"}})
	if strings.Contains(msgs[1].Text, "<previous-summary>") {
		t.Fatal("should not include previous-summary")
	}
	if !strings.Contains(msgs[1].Text, "Write a fresh handoff note") {
		t.Fatal("missing fresh instruction")
	}
}

func TestSerializeTruncatesToolOutput(t *testing.T) {
	big := strings.Repeat("x", toolSerializeMax+50)
	s := serialize([]ai.Message{{
		Role:       ai.RoleTool,
		ToolCallID: "t1",
		Text:       big,
	}})
	if !strings.Contains(s, "[truncated]") {
		t.Fatal("expected truncation marker")
	}
	if strings.Contains(s, big) {
		t.Fatal("full tool output should not appear")
	}
}

type stubCompleter struct {
	text      string
	err       error
	got       []ai.Message
	maxTokens int64
}

func (s *stubCompleter) Complete(_ context.Context, msgs []ai.Message, maxTokens int64) (string, error) {
	s.got = msgs
	s.maxTokens = maxTokens
	return s.text, s.err
}

func TestRunForcedCompacts(t *testing.T) {
	// RunForced even when under budget; window optional.
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("old stuff ", 400)},
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "recent"},
	}
	stub := &stubCompleter{text: "## Task\n- done"}
	res, err := RunForced(context.Background(), stub, hist, Config{
		Keep: estimateMsg(hist[2]) + 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Compacted {
		t.Fatal("expected compacted")
	}
	if res.TailCount != len(res.History)-1 {
		t.Fatalf("TailCount=%d want %d", res.TailCount, len(res.History)-1)
	}
	if !IsCheckpoint(res.History[0]) {
		t.Fatalf("first msg not checkpoint: %q", res.History[0].Text)
	}
	if sum, ok := ParseSummary(res.History[0]); !ok || !strings.Contains(sum, "Task") {
		t.Fatalf("summary: %q", sum)
	}
	// tail should keep the recent user message
	found := false
	for _, m := range res.History[1:] {
		if m.Text == "recent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("recent missing from tail: %+v", res.History)
	}
	if stub.got == nil {
		t.Fatal("completer not called")
	}
	if stub.maxTokens != int64(SummaryMaxTokens) {
		t.Fatalf("maxTokens=%d want %d", stub.maxTokens, SummaryMaxTokens)
	}
}

func TestRunIfNeededSkipsWhenNotNeeded(t *testing.T) {
	hist := []ai.Message{{Role: ai.RoleUser, Text: "hi"}}
	stub := &stubCompleter{text: "should not run"}
	res, err := RunIfNeeded(context.Background(), stub, hist, Config{ContextWindow: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	if res.Compacted {
		t.Fatal("should not compact")
	}
	if stub.got != nil {
		t.Fatal("completer should not be called")
	}
	if len(res.History) != 1 || res.History[0].Text != "hi" {
		t.Fatalf("history mutated: %+v", res.History)
	}
}

func TestRunForcedPropagatesCompleterError(t *testing.T) {
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("x", 200)},
		{Role: ai.RoleUser, Text: "tail"},
	}
	stub := &stubCompleter{err: errors.New("boom")}
	_, err := RunForced(context.Background(), stub, hist, Config{Keep: 10})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunForcedEmptySummary(t *testing.T) {
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("x", 200)},
		{Role: ai.RoleUser, Text: "tail"},
	}
	stub := &stubCompleter{text: "   "}
	_, err := RunForced(context.Background(), stub, hist, Config{Keep: 10})
	if err == nil || !strings.Contains(err.Error(), "empty summary") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunForcedNothingToCompact(t *testing.T) {
	// everything fits in keep budget
	hist := []ai.Message{{Role: ai.RoleUser, Text: "tiny"}}
	stub := &stubCompleter{text: "sum"}
	res, err := RunForced(context.Background(), stub, hist, Config{Keep: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	if res.Compacted {
		t.Fatal("no head → no compact")
	}
}

func TestRunForcedMergesPreviousSummary(t *testing.T) {
	cp := CheckpointMessage("## Task\n- old goal")
	// force head non-empty: large middle message, tiny tail keep
	mid := ai.Message{Role: ai.RoleUser, Text: strings.Repeat("middle ", 300)}
	tail := ai.Message{Role: ai.RoleUser, Text: "now"}
	hist := []ai.Message{cp, mid, tail}
	stub := &stubCompleter{text: "## Task\n- new goal"}
	res, err := RunForced(context.Background(), stub, hist, Config{
		Keep: estimateMsg(tail) + 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Compacted {
		t.Fatal("expected compact")
	}
	if stub.got == nil || !strings.Contains(stub.got[1].Text, "old goal") {
		t.Fatalf("prompt should include previous summary: %v", stub.got)
	}
}

func msgsText(msgs []ai.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Text
	}
	return out
}

func roles(msgs []ai.Message) []ai.Role {
	out := make([]ai.Role, len(msgs))
	for i, m := range msgs {
		out[i] = m.Role
	}
	return out
}
