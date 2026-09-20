package core

import (
	"encoding/json"
	"testing"

	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/tools"
)

func TestTodosFromRecordsLastWins(t *testing.T) {
	args1, _ := json.Marshal(map[string]any{
		"items": []map[string]any{{"id": "1", "subject": "first", "status": "pending"}},
	})
	args2, _ := json.Marshal(map[string]any{
		"items": []map[string]any{
			{"id": "1", "subject": "first", "status": "completed"},
			{"id": "2", "subject": "second", "status": "in_progress"},
		},
	})
	clear, _ := json.Marshal(map[string]any{"items": []any{}})

	recs := []session.Record{
		{Role: session.RoleUser, Text: "start"},
		{
			Role: session.RoleAgent,
			ToolCalls: []session.ToolCall{
				{ID: "c1", Name: tools.Todo, Arguments: string(args1)},
			},
		},
		{Role: session.RoleTool, Tool: tools.Todo, ToolCallID: "c1", Text: "Todos (1):\n[pending] 1: first"},
		{
			Role: session.RoleAgent,
			ToolCalls: []session.ToolCall{
				{ID: "c2", Name: tools.Todo, Arguments: string(args2)},
			},
		},
		{Role: session.RoleTool, Tool: tools.Todo, ToolCallID: "c2", Text: "Todos (2):..."},
	}
	got := TodosFromRecords(recs)
	if len(got) != 2 || got[0].Status != todo.Completed || got[1].ID != "2" {
		t.Fatalf("got=%+v", got)
	}

	// Denied call does not overwrite.
	recs = append(recs,
		session.Record{
			Role: session.RoleAgent,
			ToolCalls: []session.ToolCall{
				{ID: "c3", Name: tools.Todo, Arguments: string(clear)},
			},
		},
		session.Record{Role: session.RoleTool, Tool: tools.Todo, ToolCallID: "c3", Denied: true, Text: "rejected"},
	)
	got = TodosFromRecords(recs)
	if len(got) != 2 {
		t.Fatalf("denied should not clear: %+v", got)
	}

	// Tool error body (Denied=false) must not count as success.
	recs = append(recs,
		session.Record{
			Role: session.RoleAgent,
			ToolCalls: []session.ToolCall{
				{ID: "c3err", Name: tools.Todo, Arguments: string(clear)},
			},
		},
		session.Record{Role: session.RoleTool, Tool: tools.Todo, ToolCallID: "c3err", Text: "error: todo store unavailable"},
	)
	got = TodosFromRecords(recs)
	if len(got) != 2 {
		t.Fatalf("error body should not clear: %+v", got)
	}

	// Assistant tool_calls without a tool result (cancel mid-call) ignored.
	recs = append(recs, session.Record{
		Role: session.RoleAgent,
		ToolCalls: []session.ToolCall{
			{ID: "c3b", Name: tools.Todo, Arguments: string(clear)},
		},
	})
	got = TodosFromRecords(recs)
	if len(got) != 2 {
		t.Fatalf("incomplete should not clear: %+v", got)
	}

	// Successful clear.
	recs = append(recs,
		session.Record{
			Role: session.RoleAgent,
			ToolCalls: []session.ToolCall{
				{ID: "c4", Name: tools.Todo, Arguments: string(clear)},
			},
		},
		session.Record{Role: session.RoleTool, Tool: tools.Todo, ToolCallID: "c4", Text: "Todos (0):"},
	)
	got = TodosFromRecords(recs)
	if len(got) != 0 {
		t.Fatalf("clear=%+v", got)
	}
}

func TestSeedTodosReplacesStore(t *testing.T) {
	var s Session
	s.SeedTodos([]todo.Item{{ID: "1", Subject: "A", Status: todo.Pending}})
	if snap := s.Todos.Snapshot(); len(snap) != 1 || snap[0].ID != "1" {
		t.Fatalf("seed=%+v", snap)
	}
	s.SeedTodos(nil)
	if snap := s.Todos.Snapshot(); len(snap) != 0 {
		t.Fatalf("clear=%+v", snap)
	}
}
