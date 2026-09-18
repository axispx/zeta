package compact

import (
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/session"
)

func TestRebuildAPIHistoryWithTail(t *testing.T) {
	old := session.Record{Role: session.RoleUser, Text: strings.Repeat("old ", 500)}
	mid := session.Record{Role: session.RoleAgent, Text: "working"}
	recent := session.Record{Role: session.RoleUser, Text: "recent question"}
	tailCount := 1
	summary := "## Task\n- ship it"
	follow := session.Record{Role: session.RoleUser, Text: "and then?"}
	log := []session.Record{
		old, mid, recent,
		{Role: session.RoleCompact, Text: summary, Tail: tailCount},
		follow,
	}

	hist := RebuildAPIHistory(log)
	if len(hist) != 1+tailCount+1 {
		t.Fatalf("hist len=%d: %+v", len(hist), hist)
	}
	if !IsCheckpoint(hist[0]) {
		t.Fatalf("not checkpoint: %+v", hist[0])
	}
	if sum, ok := ParseSummary(hist[0]); !ok || sum != summary {
		t.Fatalf("summary=%q ok=%v", sum, ok)
	}
	if hist[1].Text != recent.Text {
		t.Fatalf("tail=%+v want recent", hist[1])
	}
	if hist[2].Text != follow.Text {
		t.Fatalf("follow=%+v", hist[2])
	}
}

func TestRebuildAPIHistoryEmptyTail(t *testing.T) {
	// Tail 0 keeps only the checkpoint.
	log := []session.Record{
		{Role: session.RoleUser, Text: strings.Repeat("old ", 500)},
		{Role: session.RoleUser, Text: "recent"},
		{Role: session.RoleCompact, Text: "## Task\n- x", Tail: 0},
	}
	hist := RebuildAPIHistory(log)
	if len(hist) != 1 || !IsCheckpoint(hist[0]) {
		t.Fatalf("hist=%+v", hist)
	}
}

func TestRebuildAPIHistoryImages(t *testing.T) {
	log := []session.Record{
		{
			Role: session.RoleUser,
			Text: "see this",
			Images: []session.ImageRef{
				{URL: "data:image/png;base64,AAAA", MIME: "image/png", Name: "a.png"},
			},
		},
	}
	hist := RebuildAPIHistory(log)
	if len(hist) != 1 {
		t.Fatalf("hist=%+v", hist)
	}
	if hist[0].Text != "see this" || len(hist[0].Images) != 1 || hist[0].Images[0].URL != "data:image/png;base64,AAAA" {
		t.Fatalf("msg=%+v", hist[0])
	}
}

func TestRebuildAPIHistoryNoCompact(t *testing.T) {
	log := []session.Record{
		{Role: session.RoleUser, Text: "hi"},
		{Role: session.RoleAgent, Text: "hello"},
	}
	hist := RebuildAPIHistory(log)
	if len(hist) != 2 || hist[0].Role != ai.RoleUser || hist[1].Role != ai.RoleAssistant {
		t.Fatalf("hist=%+v", hist)
	}
}

func TestRebuildAPIHistoryMultipleCompacts(t *testing.T) {
	// Sequential compact events must apply left-to-right (iterative, not last-only).
	log := []session.Record{
		{Role: session.RoleUser, Text: "a1"},
		{Role: session.RoleUser, Text: "a2"},
		{Role: session.RoleCompact, Text: "sum1", Tail: 1},
		{Role: session.RoleUser, Text: "b1"},
		{Role: session.RoleUser, Text: "b2"},
		{Role: session.RoleCompact, Text: "sum2", Tail: 1},
		{Role: session.RoleUser, Text: "c"},
	}
	hist := RebuildAPIHistory(log)
	// After second compact: checkpoint(sum2) + last 1 of then-history + "c"
	// then-history = [cp1, a2, b1, b2] → tail 1 = b2
	if len(hist) != 3 {
		t.Fatalf("hist len=%d: %+v", len(hist), hist)
	}
	if sum, ok := ParseSummary(hist[0]); !ok || sum != "sum2" {
		t.Fatalf("checkpoint: %+v", hist[0])
	}
	if hist[1].Text != "b2" {
		t.Fatalf("tail=%q want b2", hist[1].Text)
	}
	if hist[2].Text != "c" {
		t.Fatalf("follow=%q", hist[2].Text)
	}
}

// A cancelled turn persists the assistant tool_calls without results, and the
// user's next prompt is persisted after it. Rebuild must not replay that round:
// DeepSeek rejects the request with "An assistant message with 'tool_calls' must
// be followed by tool messages responding to each 'tool_call_id'".
func TestRebuildAPIHistoryCancelledToolRound(t *testing.T) {
	log := []session.Record{
		{Role: session.RoleUser, Text: "add /usage"},
		{Role: session.RoleAgent, ToolCalls: []session.ToolCall{{ID: "c1", Name: "edit"}}},
		{Role: session.RoleUser, Text: "do we need to persist it though?"},
		{Role: session.RoleAgent, ToolCalls: []session.ToolCall{{ID: "c2", Name: "bash"}}},
		{Role: session.RoleTool, ToolCallID: "c2", Text: "ok"},
		{Role: session.RoleAgent, Text: "Yes."},
	}
	hist := RebuildAPIHistory(log)
	pending := 0
	for i, m := range hist {
		switch m.Role {
		case ai.RoleUser:
			if pending > 0 {
				t.Fatalf("user at %d follows %d unanswered tool calls: %+v", i, pending, hist)
			}
		case ai.RoleAssistant:
			pending = len(m.ToolCalls)
		case ai.RoleTool:
			pending--
		}
	}
	if pending != 0 {
		t.Fatalf("unanswered tool calls at tail: %+v", hist)
	}
	// Dropped: the cancelled edit round. Kept: both user turns, the answered
	// bash round, and the final answer.
	if len(hist) != 5 {
		t.Fatalf("hist len=%d: %+v", len(hist), hist)
	}
	if hist[1].Text != "do we need to persist it though?" {
		t.Fatalf("kept the later user turn? %+v", hist)
	}
	if hist[4].Text != "Yes." {
		t.Fatalf("kept the later answer? %+v", hist)
	}
}

func TestTrimIncomplete(t *testing.T) {
	user := ai.Message{Role: ai.RoleUser, Text: "hi"}
	asst := ai.Message{Role: ai.RoleAssistant, Text: "ok"}
	asstTools := ai.Message{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{
			{ID: "1", Name: "read", Arguments: `{"path":"a"}`},
			{ID: "2", Name: "read", Arguments: `{"path":"b"}`},
		},
	}
	tool1 := ai.Message{Role: ai.RoleTool, Text: "a", ToolCallID: "1"}
	tool2 := ai.Message{Role: ai.RoleTool, Text: "b", ToolCallID: "2"}

	tests := []struct {
		name string
		in   []ai.Message
		want int
	}{
		{"complete plain", []ai.Message{user, asst}, 2},
		{"user only", []ai.Message{user}, 1},
		{"incomplete tools", []ai.Message{user, asstTools, tool1}, 1},
		{"complete tools", []ai.Message{user, asstTools, tool1, tool2, asst}, 5},
		{"assistant tools only", []ai.Message{user, asstTools}, 1},
		// Cancelled mid-turn then resumed: the user moved on before the calls
		// reported. The round is dropped, the later turns are kept.
		{"dangling then user", []ai.Message{user, asstTools, user, asst}, 3},
		{"partial then user", []ai.Message{user, asstTools, tool1, user}, 2},
		{"dangling then assistant", []ai.Message{user, asstTools, asst}, 2},
		{"orphan tool", []ai.Message{user, tool1, asst}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrimIncomplete(tt.in)
			if len(got) != tt.want {
				t.Fatalf("len=%d want %d (%#v)", len(got), tt.want, got)
			}
		})
	}
}
