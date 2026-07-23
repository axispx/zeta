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
	ts := Build()

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

func TestInspect(t *testing.T) {
	ro := Inspect()
	if len(ro) != 4 {
		t.Fatalf("inspect len: %d", len(ro))
	}
	if len(Build()) != 6 {
		t.Fatal("expected build tools")
	}
	names := map[string]bool{}
	for _, tool := range ro {
		names[tool.Name()] = true
	}
	if names["bash"] || names["edit"] || !names["read"] || !names["grep"] || !names["websearch"] || !names["webfetch"] {
		t.Fatalf("inspect names: %v", names)
	}
}

func TestRunModeGate(t *testing.T) {
	ro := Inspect()
	root := t.TempDir()
	out := Run(context.Background(), ro, root, "edit", mustRaw(t, map[string]any{
		"path": "x", "old_string": "", "new_string": "y",
	}))
	if !strings.Contains(out, "not available in this mode") {
		t.Fatalf("got %s", out)
	}
	out = Run(context.Background(), ro, root, "bash", mustRaw(t, map[string]any{
		"command": "echo hi",
	}))
	if !strings.Contains(out, "not available in this mode") {
		t.Fatalf("bash gate: %s", out)
	}
}

func TestSummary(t *testing.T) {
	if got := (readTool{}).Summary(mustRaw(t, map[string]any{"path": "a.go"})); got != "read a.go" {
		t.Fatal(got)
	}
	if got := (editTool{}).Summary(mustRaw(t, map[string]any{"path": "b.go"})); got != "edit b.go" {
		t.Fatal(got)
	}
	if got := (bashTool{}).Summary(mustRaw(t, map[string]any{"command": "go test ./..."})); got != "bash go test ./..." {
		t.Fatal(got)
	}
}

func TestReadDir(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	ts := Build()

	_ = os.MkdirAll(filepath.Join(root, "internal", "tools"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "internal", "tools", "read.go"), []byte("package tools\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("# zeta\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, ".git", "objects"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "empty"), 0o755)

	out := Run(ctx, ts, root, "read", mustRaw(t, map[string]any{"path": "."}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "internal/") || !strings.Contains(out, "README.md") {
		t.Fatalf("list: %q", out)
	}
	if strings.Contains(out, "read.go") {
		t.Fatalf("should not recurse: %q", out)
	}
	if strings.Contains(out, ".git") {
		t.Fatalf("should skip .git: %q", out)
	}

	out = Run(ctx, ts, root, "read", mustRaw(t, map[string]any{"path": "internal"}))
	if out != "tools/" {
		t.Fatalf("subdir: %q", out)
	}

	out = Run(ctx, ts, root, "read", mustRaw(t, map[string]any{"path": "empty"}))
	if out != "[empty directory]" {
		t.Fatalf("empty: %q", out)
	}

	out = Run(ctx, ts, root, "read", mustRaw(t, map[string]any{
		"path": ".", "offset": 1,
	}))
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "only to files") {
		t.Fatalf("dir offset: %q", out)
	}
}

func TestBashProgress(t *testing.T) {
	root := t.TempDir()
	var got []string
	ctx := WithProgress(context.Background(), func(s string) {
		got = append(got, s)
	})
	out := Run(ctx, Build(), root, "bash", mustRaw(t, map[string]any{
		"command": "printf 'one\\ntwo\\nthree\\n'",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if len(got) == 0 {
		t.Fatal("expected progress callbacks")
	}
	if !strings.Contains(got[len(got)-1], "three") {
		t.Fatalf("last progress=%q", got[len(got)-1])
	}
	if !strings.Contains(out, "three") || !strings.Contains(out, "exit: 0") {
		t.Fatalf("out: %q", out)
	}
}

func TestBash(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	ts := Build()
	_ = os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "sub", "note.txt"), []byte("hi\n"), 0o644)

	out := Run(ctx, ts, root, "bash", mustRaw(t, map[string]any{
		"command": "echo hello",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "exit: 0") {
		t.Fatalf("echo: %q", out)
	}

	out = Run(ctx, ts, root, "bash", mustRaw(t, map[string]any{
		"command": "cat note.txt", "workdir": "sub",
	}))
	if !strings.Contains(out, "hi") || !strings.Contains(out, "exit: 0") {
		t.Fatalf("workdir: %q", out)
	}

	out = Run(ctx, ts, root, "bash", mustRaw(t, map[string]any{
		"command": "echo x", "workdir": "../outside",
	}))
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "outside the workspace") {
		t.Fatalf("escape: %q", out)
	}

	out = Run(ctx, ts, root, "bash", mustRaw(t, map[string]any{
		"command": "echo x", "workdir": "missing",
	}))
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "does not exist") {
		t.Fatalf("missing workdir: %q", out)
	}

	out = Run(ctx, ts, root, "bash", mustRaw(t, map[string]any{
		"command": "echo x", "workdir": "sub/note.txt",
	}))
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "not a directory") {
		t.Fatalf("file workdir: %q", out)
	}

	out = Run(ctx, ts, root, "bash", mustRaw(t, map[string]any{
		"command": "exit 7",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "exit: 7") {
		t.Fatalf("nonzero: %q", out)
	}

	out = Run(ctx, ts, root, "bash", mustRaw(t, map[string]any{
		"command": "sleep 2", "timeout_ms": 200,
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "exit: timeout") {
		t.Fatalf("timeout: %q", out)
	}
}

func TestUserShell(t *testing.T) {
	t.Setenv("SHELL", "/nonexistent/shell")
	got := userShell()
	if got == "/nonexistent/shell" {
		t.Fatalf("should fall back, got %q", got)
	}
	if p, err := exec.LookPath("bash"); err == nil {
		if got != p && got != "sh" {
			t.Fatalf("fallback: %q", got)
		}
	}

	sh := t.TempDir() + "/mysh"
	_ = os.WriteFile(sh, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("SHELL", sh)
	if got := userShell(); got != sh {
		t.Fatalf("got %q want %q", got, sh)
	}
}

func TestCappedBuffer(t *testing.T) {
	c := &cappedBuffer{limit: 8}
	n, err := c.Write([]byte("hello world"))
	if err != nil || n != 11 {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	if !c.truncated || c.String() != "hello wo" {
		t.Fatalf("got %q truncated=%v", c.String(), c.truncated)
	}
	n, err = c.Write([]byte("more"))
	if err != nil || n != 4 || c.String() != "hello wo" {
		t.Fatalf("discard: n=%d err=%v out=%q", n, err, c.String())
	}
}

func TestBashTruncation(t *testing.T) {
	root := t.TempDir()
	// Emit more than maxBashBytes via a compact shell loop.
	out := Run(context.Background(), Build(), root, "bash", mustRaw(t, map[string]any{
		"command": "dd if=/dev/zero bs=1024 count=300 2>/dev/null | tr '\\0' a",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "[truncated:") {
		t.Fatalf("expected truncation note: %q", out[:min(120, len(out))])
	}
	if !strings.Contains(out, "exit: 0") {
		t.Fatalf("exit: %q", out[len(out)-40:])
	}
}

func TestGrep(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
	}
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\nfunc Hello() {}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "b.txt"), []byte("Hello world\n"), 0o644)

	out := Run(context.Background(), Build(), root, "grep", mustRaw(t, map[string]any{
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

	out = Run(context.Background(), Build(), root, "grep", mustRaw(t, map[string]any{
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
