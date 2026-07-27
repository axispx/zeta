package tui

import (
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
	if err := sess.AppendTodos([]todo.Item{{ID: "1", Subject: "restored", Status: todo.InProgress}}); err != nil {
		t.Fatal(err)
	}
	// Reload so Session.Todos is populated the way OpenID would.
	sess, recs, err := session.OpenID(proj, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.applySession(sess, recs, nil)
	snap := m.todos.Snapshot()
	if len(snap) != 1 || snap[0].ID != "1" || snap[0].Subject != "restored" {
		t.Fatalf("restored=%+v", snap)
	}

	m.startNewSession()
	if len(m.todos.Snapshot()) != 0 {
		t.Fatalf("new session should clear todos: %+v", m.todos.Snapshot())
	}
}

func TestTodoOnChangePersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := t.TempDir()

	sess, err := session.New(proj)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the session file so AppendTodos has a parent.
	if err := sess.Append(session.Record{Role: session.RoleUser, Text: "hi"}); err != nil {
		t.Fatal(err)
	}

	m := testModel()
	m.sess = sess
	m.wireTodos(nil)

	if _, err := m.todos.Replace([]todo.Item{
		{ID: "1", Subject: "persist me", Status: todo.Completed},
	}); err != nil {
		t.Fatal(err)
	}

	// Live session updated.
	if got := m.sess.Todos(); len(got) != 1 || got[0].Subject != "persist me" {
		t.Fatalf("live=%+v", got)
	}

	// Reloaded from disk.
	s2, _, err := session.OpenID(proj, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Todos(); len(got) != 1 || got[0].Subject != "persist me" {
		t.Fatalf("reloaded todos=%+v", got)
	}
}

func TestRenderTodoCall(t *testing.T) {
	out := stripANSI(renderTodoCall(Message{
		Role:   RoleTool,
		Tool:   tools.Todo,
		Status: ToolOK,
		Out:    "Todos (2):\n[in_progress] 1: Wire store\n[pending] 2: Persist — detail\nwarning: 2 items in_progress",
	}))
	if !strings.HasPrefix(out, "Todos\n") {
		t.Fatalf("header: %q", out)
	}
	if !strings.Contains(out, "◐ Wire store") || !strings.Contains(out, "○ Persist — detail") {
		t.Fatalf("items: %q", out)
	}
	if !strings.Contains(out, "warning: 2 items in_progress") {
		t.Fatalf("warning: %q", out)
	}
	// keepOut path
	if !toolHasOut(tools.Todo) {
		t.Fatal("todo should keepOut")
	}
	if viewFor(tools.Todo).segment != "todo" {
		t.Fatal("segment")
	}
}
