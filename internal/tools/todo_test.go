package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/todo"
)

func TestTodoToolAlwaysRegistered(t *testing.T) {
	if _, ok := ByName(Build(), Todo); !ok {
		t.Fatal("Build() should include todo")
	}
	if _, ok := ByName(Inspect(), Todo); !ok {
		t.Fatal("Inspect() should include todo")
	}
	if Interactive(Todo) {
		t.Fatal("todo must not be interactive")
	}
}

func TestTodoToolNilStore(t *testing.T) {
	out := Run(context.Background(), Build(), t.TempDir(), Todo, mustRaw(t, map[string]any{
		"items": []map[string]any{{"id": "1", "subject": "x"}},
	}))
	if !strings.Contains(out, "todo store unavailable") {
		t.Fatalf("got %s", out)
	}
}

func TestTodoToolReplace(t *testing.T) {
	store := todo.NewStore()
	ts := BuildWith(store)
	ctx := context.Background()

	out := Run(ctx, ts, t.TempDir(), Todo, mustRaw(t, map[string]any{
		"items": []map[string]any{
			{"id": "1", "subject": "Wire store", "status": "in_progress"},
			{"id": "2", "subject": "Persist", "status": "pending"},
		},
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "Todos (2):") || !strings.Contains(out, "[in_progress] 1: Wire store") {
		t.Fatalf("replace: %s", out)
	}

	out = Run(ctx, ts, t.TempDir(), Todo, mustRaw(t, map[string]any{
		"items": []map[string]any{
			{"id": "1", "subject": "Wire store", "status": "completed"},
			{"id": "2", "subject": "Persist", "status": "pending"},
			{"id": "3", "subject": "Tests", "status": "pending"},
		},
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "Todos (3):") || !strings.Contains(out, "[completed] 1: Wire store") {
		t.Fatalf("full replace: %s", out)
	}
	snap := store.Snapshot()
	if len(snap) != 3 || snap[0].Status != todo.Completed {
		t.Fatalf("store=%+v", snap)
	}

	out = Run(ctx, ts, t.TempDir(), Todo, mustRaw(t, map[string]any{"items": []any{}}))
	if strings.HasPrefix(out, "error:") || !strings.Contains(out, "Todos (0):") {
		t.Fatalf("clear: %s", out)
	}
}

func TestTodoToolInvalidArgs(t *testing.T) {
	store := todo.NewStore()
	ts := BuildWith(store)
	ctx := context.Background()
	out := Run(ctx, ts, t.TempDir(), Todo, mustRaw(t, map[string]any{
		"items": []map[string]any{{"id": "1", "subject": "x", "status": "nope"}},
	}))
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("want error, got %s", out)
	}
	out = Run(ctx, ts, t.TempDir(), Todo, mustRaw(t, map[string]any{}))
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("missing items: %s", out)
	}
}

func TestTodoToolSummary(t *testing.T) {
	tr := todoTool{}
	if got := tr.Summary(mustRaw(t, map[string]any{
		"items": []map[string]any{{"id": "1"}, {"id": "2"}, {"id": "3"}},
	})); got != "todo 3 items" {
		t.Fatal(got)
	}
}
