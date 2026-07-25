package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewBash(t *testing.T) {
	// Bash has no Previewer — Summary already carries the full command for the transcript.
	got := Preview(Build(), "bash", t.TempDir(), mustRaw(t, map[string]any{
		"command": "go test ./...",
	}))
	if got != "" {
		t.Fatalf("bash preview should be empty (label has the command): %q", got)
	}
}

func TestPreviewEditCreate(t *testing.T) {
	root := t.TempDir()
	got := Preview(Build(), "edit", root, mustRaw(t, map[string]any{
		"path": "new.md", "old_string": "", "new_string": "hello\nworld\n",
	}))
	if !strings.Contains(got, "--- new.md") || !strings.Contains(got, "+hello") {
		t.Fatalf("diff: %s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "new.md")); !os.IsNotExist(err) {
		t.Fatal("preview must not write")
	}
}

func TestPreviewEditModify(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "f.txt")
	if err := os.WriteFile(path, []byte("aaa\nbbb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Preview(Build(), "edit", root, mustRaw(t, map[string]any{
		"path": "f.txt", "old_string": "bbb", "new_string": "BBB",
	}))
	if !strings.Contains(got, "-bbb") || !strings.Contains(got, "+BBB") {
		t.Fatalf("diff: %s", got)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "aaa\nbbb\n" {
		t.Fatalf("file mutated: %s", data)
	}
}

func TestPreviewEditExistingEmptyRejects(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "e.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := Preview(Build(), "edit", root, mustRaw(t, map[string]any{
		"path": "e.txt", "old_string": "", "new_string": "x\n",
	}))
	if got != "create e.txt" {
		t.Fatalf("preview fallback=%q", got)
	}
}

func TestEditExistingEmptyErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "empty.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out := Run(t.Context(), Build(), root, "edit", mustRaw(t, map[string]any{
		"path": "empty.txt", "old_string": "", "new_string": "hello\n",
	}))
	if !strings.Contains(out, "error:") || !strings.Contains(out, "write") {
		t.Fatalf("expected write hint, got %s", out)
	}
}

func TestWriteCreateAndOverwrite(t *testing.T) {
	root := t.TempDir()
	ctx := t.Context()
	ts := Build()

	out := Run(ctx, ts, root, "write", mustRaw(t, map[string]any{
		"path": "a.txt", "content": "one\n",
	}))
	if !strings.Contains(out, "+one") {
		t.Fatalf("create: %s", out)
	}
	if (writeTool{}).Summary(mustRaw(t, map[string]any{"path": "a.txt", "content": "x"})) != "write a.txt" {
		t.Fatal("write summary")
	}
	if (writeTool{}).Summary(mustRaw(t, map[string]any{"path": "b.txt", "content": "x"})) != "write b.txt" {
		t.Fatal("write always summarizes as write")
	}

	out = Run(ctx, ts, root, "write", mustRaw(t, map[string]any{
		"path": "a.txt", "content": "two\n",
	}))
	if !strings.Contains(out, "-one") || !strings.Contains(out, "+two") {
		t.Fatalf("overwrite: %s", out)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "two\n" {
		t.Fatalf("content: %q", data)
	}
}

func TestWriteEmptyFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "e.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out := Run(t.Context(), Build(), root, "write", mustRaw(t, map[string]any{
		"path": "e.txt", "content": "filled\n",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatalf("write empty: %s", out)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "filled\n" {
		t.Fatalf("content: %q", data)
	}
}

func TestPreviewWrite(t *testing.T) {
	root := t.TempDir()
	got := Preview(Build(), "write", root, mustRaw(t, map[string]any{
		"path": "n.txt", "content": "hi\n",
	}))
	if !strings.Contains(got, "+hi") {
		t.Fatalf("preview: %s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "n.txt")); !os.IsNotExist(err) {
		t.Fatal("preview must not write")
	}
}

func TestPreviewEditFallback(t *testing.T) {
	root := t.TempDir()
	got := Preview(Build(), "edit", root, mustRaw(t, map[string]any{
		"path": "missing.txt", "old_string": "x", "new_string": "y",
	}))
	if got != "edit missing.txt" {
		t.Fatalf("fallback=%q", got)
	}
}

func TestPreviewMissingFromSet(t *testing.T) {
	got := Preview(Inspect(), "edit", t.TempDir(), mustRaw(t, map[string]any{
		"path": "a.txt", "old_string": "", "new_string": "x",
	}))
	if got != "" {
		t.Fatalf("inspect set has no edit: %q", got)
	}
}

func TestEditSummary(t *testing.T) {
	if got := (editTool{}).Summary(mustRaw(t, map[string]any{"path": "a.go", "old_string": "", "new_string": "x"})); got != "create a.go" {
		t.Fatalf("create: %q", got)
	}
	if got := (editTool{}).Summary(mustRaw(t, map[string]any{"path": "a.go", "old_string": "a", "new_string": "b"})); got != "edit a.go" {
		t.Fatalf("edit: %q", got)
	}
}
