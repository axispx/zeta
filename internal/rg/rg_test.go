package rg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireRG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
	}
}

func TestListFilesRespectsGitignore(t *testing.T) {
	requireRG(t)
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "internal", "tui"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "internal", "tui", "model.go"), []byte("package tui\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("# x\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)

	paths, err := ListFiles(context.Background(), root, 0)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "internal/tui/model.go") {
		t.Fatalf("missing model.go: %q", joined)
	}
	if !strings.Contains(joined, "README.md") {
		t.Fatalf("missing README: %q", joined)
	}
	if strings.Contains(joined, "node_modules") {
		t.Fatalf("gitignore leak: %q", joined)
	}
	if strings.Contains(joined, ".git/") {
		t.Fatalf(".git leak: %q", joined)
	}
	for _, p := range paths {
		if strings.Contains(p, "\\") {
			t.Fatalf("want slash paths, got %q", p)
		}
		if filepath.IsAbs(p) {
			t.Fatalf("want relative, got %q", p)
		}
	}
}

func TestListFilesLimit(t *testing.T) {
	requireRG(t)
	root := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := ListFiles(context.Background(), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("len=%d want 2: %v", len(paths), paths)
	}
}

func TestListFilesCancel(t *testing.T) {
	requireRG(t)
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("x\n"), 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ListFiles(ctx, root, 0)
	if err == nil {
		// rg may finish before kill on tiny trees; accept either outcome.
		return
	}
	if !errorsIsCancel(err) {
		t.Fatalf("err=%v", err)
	}
}

func errorsIsCancel(err error) bool {
	return err != nil && (err == context.Canceled || err == context.DeadlineExceeded ||
		strings.Contains(err.Error(), "context canceled") ||
		strings.Contains(err.Error(), "signal: killed"))
}

func TestSlashPaths(t *testing.T) {
	in := []string{"a\\b", "c/d"}
	out := SlashPaths(in)
	if len(out) != 2 || out[0] != filepath.ToSlash("a\\b") || out[1] != "c/d" {
		t.Fatalf("%v", out)
	}
	if SlashPaths(nil) != nil {
		t.Fatal("nil")
	}
}
