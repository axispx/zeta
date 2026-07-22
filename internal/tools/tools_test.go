package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePath(t *testing.T) {
	root := t.TempDir()
	abs, err := resolvePath(root, "foo/bar.go")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "foo", "bar.go")
	if abs != want {
		t.Fatalf("got %q want %q", abs, want)
	}
	if _, err := resolvePath(root, "../outside"); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestReadEdit(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	ts := All()

	create := mustRaw(t, map[string]any{
		"path": "hello.txt", "old_string": "", "new_string": "one\ntwo\nthree\n",
	})
	out := Run(ctx, ts, root, "edit", create)
	if !strings.Contains(out, "created") {
		t.Fatalf("create: %s", out)
	}

	readOut := Run(ctx, ts, root, "read", mustRaw(t, map[string]any{"path": "hello.txt"}))
	if !strings.Contains(readOut, "one") || !strings.Contains(readOut, "1|") {
		t.Fatalf("read: %s", readOut)
	}

	edit := mustRaw(t, map[string]any{
		"path": "hello.txt", "old_string": "two", "new_string": "TWO",
	})
	out = Run(ctx, ts, root, "edit", edit)
	if !strings.Contains(out, "edited") {
		t.Fatalf("edit: %s", out)
	}
	data, _ := os.ReadFile(filepath.Join(root, "hello.txt"))
	if !strings.Contains(string(data), "TWO") {
		t.Fatalf("file content: %s", data)
	}

	// ambiguous without replace_all
	_ = os.WriteFile(filepath.Join(root, "dup.txt"), []byte("a a a"), 0o644)
	out = Run(ctx, ts, root, "edit", mustRaw(t, map[string]any{
		"path": "dup.txt", "old_string": "a", "new_string": "b",
	}))
	if !strings.Contains(out, "error:") {
		t.Fatalf("expected uniqueness error, got %s", out)
	}
}

func TestReadOnly(t *testing.T) {
	ro := ReadOnly(All())
	for _, tool := range ro {
		if tool.Access() == AccessWrite {
			t.Fatalf("read-only set includes %s", tool.Name())
		}
	}
	if len(All()) != 3 {
		t.Fatal("expected all tools")
	}
}

func TestRunModeGate(t *testing.T) {
	out := Run(context.Background(), ReadOnly(All()), t.TempDir(), "edit", mustRaw(t, map[string]any{
		"path": "x", "old_string": "", "new_string": "y",
	}))
	if !strings.Contains(out, "not available in this mode") {
		t.Fatalf("got %s", out)
	}
}

func TestSummary(t *testing.T) {
	if got := (readTool{}).Summary(mustRaw(t, map[string]any{"path": "a.go"})); got != "read a.go" {
		t.Fatal(got)
	}
	if got := (editTool{}).Summary(mustRaw(t, map[string]any{"path": "b.go"})); got != "edit b.go" {
		t.Fatal(got)
	}
}

func TestGrep(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
	}
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\nfunc Hello() {}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "b.txt"), []byte("Hello world\n"), 0o644)

	out := Run(context.Background(), All(), root, "grep", mustRaw(t, map[string]any{
		"pattern": "Hello",
		"glob":    "*.go",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "Hello") {
		t.Fatalf("got %q", out)
	}
	if strings.Contains(out, "b.txt") {
		t.Fatalf("glob leaked: %q", out)
	}

	out = Run(context.Background(), All(), root, "grep", mustRaw(t, map[string]any{
		"pattern": "nomatch_xyz",
	}))
	if out != "no matches" {
		t.Fatalf("got %q", out)
	}
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
