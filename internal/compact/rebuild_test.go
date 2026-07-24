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
	summary := "## Objective\n- ship it"
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

func TestRebuildAPIHistoryLegacyNoTail(t *testing.T) {
	// Tail==0 falls back to Select(DefaultKeep).
	old := session.Record{Role: session.RoleUser, Text: strings.Repeat("old ", 500)}
	recent := session.Record{Role: session.RoleUser, Text: "recent"}
	pre := []ai.Message{
		{Role: ai.RoleUser, Text: old.Text},
		{Role: ai.RoleUser, Text: recent.Text},
	}
	wantTail := Select(pre, DefaultKeep).Tail
	log := []session.Record{
		old, recent,
		{Role: session.RoleCompact, Text: "## Objective\n- x", Tail: 0},
	}
	hist := RebuildAPIHistory(log)
	if len(hist) != 1+len(wantTail) {
		t.Fatalf("hist len=%d want %d", len(hist), 1+len(wantTail))
	}
	for i, m := range wantTail {
		if hist[1+i].Text != m.Text {
			t.Fatalf("tail[%d]=%q want %q", i, hist[1+i].Text, m.Text)
		}
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
