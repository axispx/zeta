package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlob(t *testing.T) {
	requireRG(t)
	root := t.TempDir()
	ctx := context.Background()
	ts := Build()

	_ = os.MkdirAll(filepath.Join(root, "internal", "tools"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "internal", "tools", "read.go"), []byte("package tools\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "internal", "tools", "glob.go"), []byte("package tools\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("# zeta\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, ".git", "objects"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.go"), []byte("package pkg\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n"), 0o644)

	out := Run(ctx, ts, root, Glob, mustRaw(t, map[string]any{
		"pattern": "**/*.go",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "internal/tools/read.go") || !strings.Contains(out, "internal/tools/glob.go") {
		t.Fatalf("go files: %q", out)
	}
	if strings.Contains(out, "README.md") || strings.Contains(out, ".git") {
		t.Fatalf("unexpected match: %q", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Fatalf("gitignore ignored: %q", out)
	}
	if strings.Contains(out, root) {
		t.Fatalf("expected relative paths, got %q", out)
	}

	out = Run(ctx, ts, root, Glob, mustRaw(t, map[string]any{
		"pattern": "*.md",
	}))
	if out != "README.md" {
		t.Fatalf("basename glob: %q", out)
	}

	out = Run(ctx, ts, root, Glob, mustRaw(t, map[string]any{
		"pattern": "*.go",
		"path":    "internal",
	}))
	if !strings.Contains(out, "internal/tools/read.go") {
		t.Fatalf("scoped search: %q", out)
	}
	if strings.Contains(out, "README.md") {
		t.Fatalf("scope leaked: %q", out)
	}

	out = Run(ctx, ts, root, Glob, mustRaw(t, map[string]any{
		"pattern": "missing_*",
	}))
	if out != "no matches" {
		t.Fatalf("no matches: %q", out)
	}

	out = Run(ctx, ts, root, Glob, mustRaw(t, map[string]any{
		"pattern": "*.go",
		"path":    "README.md",
	}))
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "not a directory") {
		t.Fatalf("file path: %q", out)
	}
}

func TestGlobSummary(t *testing.T) {
	if got := (globTool{}).Summary(mustRaw(t, map[string]any{"pattern": "**/*.go"})); got != "glob **/*.go" {
		t.Fatal(got)
	}
	if got := (globTool{}).Summary(mustRaw(t, map[string]any{"pattern": "*.go", "path": "internal"})); got != "glob *.go internal" {
		t.Fatal(got)
	}
}

func TestGlobCancel(t *testing.T) {
	requireRG(t)
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := Run(ctx, Build(), root, Glob, mustRaw(t, map[string]any{
		"pattern": "*.go",
	}))
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("expected cancel error, got %q", out)
	}
}
