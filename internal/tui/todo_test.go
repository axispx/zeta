package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

func TestApplySessionRestoresAndClearsTodos(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := t.TempDir()

	m := testModel()
	m.session.WS = workspace.Context{Abs: proj}
	m.session.Todos = todo.NewStore()
	if _, err := m.session.Todos.Replace([]todo.Item{{ID: "old", Subject: "stale"}}); err != nil {
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
	snap := m.session.Todos.Snapshot()
	if len(snap) != 1 || snap[0].ID != "1" || snap[0].Subject != "restored" {
		t.Fatalf("restored=%+v", snap)
	}

	// Transcript keeps Format body for the todo row.
	var found bool
	for _, msg := range m.transcript.messages {
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
	if len(m.session.Todos.Snapshot()) != 0 {
		t.Fatalf("new session should clear todos: %+v", m.session.Todos.Snapshot())
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
