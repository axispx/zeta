package agent

import (
	"testing"

	"github.com/axispx/zeta/internal/ai"
)

func TestExecToolInvalidJSON(t *testing.T) {
	c := Config{Tools: nil, Root: t.TempDir()}
	label, result := c.execTool(t.Context(), ai.ToolCall{
		ID:        "call_1",
		Name:      "read",
		Arguments: `{`,
	})
	if label != "read" {
		t.Fatalf("label=%q", label)
	}
	if result.ToolCallID != "call_1" {
		t.Fatal(result)
	}
	if result.Text != "error: invalid JSON arguments" {
		t.Fatalf("text=%q", result.Text)
	}
}
