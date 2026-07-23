package agent

import (
	"testing"
	"time"

	"github.com/axispx/zeta/internal/ai"
)

func TestExecToolInvalidJSON(t *testing.T) {
	c := Config{Tools: nil, Root: t.TempDir()}
	ev := make(chan Event, 4)
	label, result := c.execTool(t.Context(), ai.ToolCall{
		ID:        "call_1",
		Name:      "read",
		Arguments: `{`,
	}, ev)
	if label != "read" {
		t.Fatalf("label=%q", label)
	}
	if result.ToolCallID != "call_1" {
		t.Fatal(result)
	}
	if result.Text != "error: invalid JSON arguments" {
		t.Fatalf("text=%q", result.Text)
	}
	select {
	case got := <-ev:
		if got.Kind != KindToolStart || got.Name != "read" || got.Text != "read" {
			t.Fatalf("start event: %+v", got)
		}
	default:
		t.Fatal("expected KindToolStart")
	}
}

func TestEmitToolOutDropsWhenFull(t *testing.T) {
	ev := make(chan Event) // unbuffered; send must not block
	done := make(chan struct{})
	go func() {
		emitToolOut(ev, "bash", "line")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emitToolOut blocked")
	}
}
