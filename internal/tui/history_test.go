package tui

import (
	"testing"

	"github.com/axispx/zeta/internal/ai"
)

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
			got := trimIncomplete(tt.in)
			if len(got) != tt.want {
				t.Fatalf("len=%d want %d (%#v)", len(got), tt.want, got)
			}
		})
	}
}
