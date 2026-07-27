package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

func TestRequestMsgsTodosBlock(t *testing.T) {
	hist := []ai.Message{{Role: ai.RoleUser, Text: "go"}}
	// empty store → no block
	empty := todo.NewStore()
	msgs := requestMsgs(workspace.Context{}, prompt.ModeBuild, hist, empty)
	for _, m := range msgs {
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "# Session todos") {
			t.Fatalf("unexpected todos block: %q", m.Text)
		}
	}

	store := todo.NewStore()
	if _, err := store.Replace([]todo.Item{
		{ID: "1", Subject: "A", Status: todo.Pending},
	}); err != nil {
		t.Fatal(err)
	}
	msgs = requestMsgs(workspace.Context{}, prompt.ModeBuild, hist, store)
	var saw bool
	// should appear after mode developer, before history
	modeIdx, todoIdx, userIdx := -1, -1, -1
	for i, m := range msgs {
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "# Mode: Build") {
			modeIdx = i
		}
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "# Session todos") {
			todoIdx = i
			saw = true
			if !strings.Contains(m.Text, "- [ ] **1**: A") {
				t.Fatalf("block=%q", m.Text)
			}
		}
		if m.Role == ai.RoleUser {
			userIdx = i
		}
	}
	if !saw || modeIdx < 0 || todoIdx != modeIdx+1 || userIdx != todoIdx+1 {
		t.Fatalf("order mode=%d todo=%d user=%d saw=%v roles=%v", modeIdx, todoIdx, userIdx, saw, rolesOf(msgs))
	}
}

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
	got := todosFromRecords(recs)
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
	got = todosFromRecords(recs)
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
	got = todosFromRecords(recs)
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
	got = todosFromRecords(recs)
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
	got = todosFromRecords(recs)
	if len(got) != 0 {
		t.Fatalf("clear=%+v", got)
	}
}

func TestApplySessionRestoresAndClearsTodos(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := t.TempDir()

	m := testModel()
	m.ws = workspace.Context{Abs: proj}
	m.todos = todo.NewStore()
	if _, err := m.todos.Replace([]todo.Item{{ID: "old", Subject: "stale"}}); err != nil {
		t.Fatal(err)
	}

	sess, err := session.New(proj)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{
		"items": []map[string]any{{"id": "1", "subject": "restored", "status": "in_progress"}},
	})
	if err := sess.Append(session.Record{Role: session.RoleUser, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(session.Record{
		Role: session.RoleAgent,
		ToolCalls: []session.ToolCall{
			{ID: "t1", Name: tools.Todo, Arguments: string(args)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(session.Record{
		Role:       session.RoleTool,
		Tool:       tools.Todo,
		ToolCallID: "t1",
		Text:       "Todos (1):\n[in_progress] 1: restored",
		Label:      "todo 1 items",
	}); err != nil {
		t.Fatal(err)
	}

	sess, recs, err := session.OpenID(proj, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.applySession(sess, recs, nil)
	snap := m.todos.Snapshot()
	if len(snap) != 1 || snap[0].ID != "1" || snap[0].Subject != "restored" {
		t.Fatalf("restored=%+v", snap)
	}

	// Transcript keeps Format body for the todo row.
	var found bool
	for _, msg := range m.messages {
		if msg.Role == RoleTool && msg.Tool == tools.Todo {
			found = true
			if !strings.Contains(msg.Out, "Todos (1):") {
				t.Fatalf("Out=%q", msg.Out)
			}
		}
	}
	if !found {
		t.Fatal("missing todo tool row")
	}

	m.startNewSession()
	if len(m.todos.Snapshot()) != 0 {
		t.Fatalf("new session should clear todos: %+v", m.todos.Snapshot())
	}
}

func TestRenderTodoCall(t *testing.T) {
	out := stripANSI(renderTodoCall(Message{
		Role:   RoleTool,
		Tool:   tools.Todo,
		Status: ToolOK,
		Out:    "Todos (2):\n[in_progress] 1: Wire store\n[pending] 2: Persist — detail",
	}))
	if !strings.Contains(out, "Todos (2):") || !strings.Contains(out, "[in_progress] 1: Wire store") {
		t.Fatalf("body: %q", out)
	}
	denied := stripANSI(renderTodoCall(Message{
		Role: RoleTool, Tool: tools.Todo, Status: ToolDenied,
	}))
	if !strings.Contains(denied, "todo") || !strings.Contains(denied, "denied") {
		t.Fatalf("denied: %q", denied)
	}
	if !toolHasOut(tools.Todo) {
		t.Fatal("todo should keepOut")
	}
	if viewFor(tools.Todo).segment != "todo" {
		t.Fatal("segment")
	}
}
