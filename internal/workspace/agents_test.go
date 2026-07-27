package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectNearestAgents(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "root agents")
	sub := filepath.Join(root, "pkg", "api")
	mustMkdir(t, sub)
	mustWrite(t, filepath.Join(sub, "AGENTS.md"), "nested agents")

	_, agents := inspect(sub)
	if agents != "nested agents" {
		t.Fatalf("inspect(sub) agents = %q, want nested agents", agents)
	}
}

func TestInspectWalksToGitRoot(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "root agents")
	sub := filepath.Join(root, "pkg", "api")
	mustMkdir(t, sub)

	branch, agents := inspect(sub)
	if agents != "root agents" {
		t.Fatalf("inspect(sub) agents = %q, want root agents", agents)
	}
	if branch != "main" {
		t.Fatalf("inspect(sub) branch = %q, want main", branch)
	}
}

func TestInspectStopsAtGitRoot(t *testing.T) {
	outer := t.TempDir()
	mustWrite(t, filepath.Join(outer, "AGENTS.md"), "outer agents")
	root := filepath.Join(outer, "repo")
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/dev\n")
	sub := filepath.Join(root, "src")
	mustMkdir(t, sub)

	branch, agents := inspect(sub)
	if agents != "" {
		t.Fatalf("inspect(sub) agents = %q, want empty (should not escape git root)", agents)
	}
	if branch != "dev" {
		t.Fatalf("inspect(sub) branch = %q, want dev", branch)
	}
}

func TestInspectNoGitWalksUp(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "parent agents")
	nested := filepath.Join(dir, "nested")
	mustMkdir(t, nested)

	if _, agents := inspect(dir); agents != "parent agents" {
		t.Fatalf("inspect(dir) agents = %q, want parent agents", agents)
	}
	branch, agents := inspect(nested)
	if agents != "parent agents" {
		t.Fatalf("inspect(nested) agents = %q, want parent agents (walk toward /)", agents)
	}
	if branch != "" {
		t.Fatalf("inspect(nested) branch = %q, want empty", branch)
	}
}

func TestRefreshBranch(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "keep me")

	c := Context{Abs: root, Cwd: root, Branch: "main", AgentsMD: "keep me"}
	mustWrite(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/feature\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "changed")
	c.RefreshBranch()
	if c.Branch != "feature" {
		t.Fatalf("RefreshBranch branch = %q, want feature", c.Branch)
	}
	if c.AgentsMD != "keep me" {
		t.Fatalf("RefreshBranch must not reload AGENTS.md, got %q", c.AgentsMD)
	}
}

func TestInspectEmptyIgnored(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "  \n  ")
	if _, agents := inspect(dir); agents != "" {
		t.Fatalf("inspect agents = %q, want empty for whitespace-only file", agents)
	}
}

func TestInspectTruncates(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("a", maxAgentsMD+100)
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), big)

	_, agents := inspect(dir)
	if !strings.HasSuffix(agents, "[truncated]") {
		t.Fatalf("expected truncation marker, got len=%d", len(agents))
	}
	if len(agents) > maxAgentsMD+len("\n\n[truncated]") {
		t.Fatalf("truncated body too large: %d", len(agents))
	}
}

func TestTrimIncompleteUTF8(t *testing.T) {
	// "é" is 2 bytes (C3 A9); cut after first byte.
	cut := "ab\xc3"
	got := trimIncompleteUTF8(cut)
	if got != "ab" {
		t.Fatalf("trimIncompleteUTF8(%q) = %q, want %q", cut, got, "ab")
	}
	// Mid-string invalid byte must not be eaten by peeling the end.
	mid := "ok\xffstill"
	if got := trimIncompleteUTF8(mid); got != mid {
		t.Fatalf("trimIncompleteUTF8 mid-invalid = %q, want unchanged %q", got, mid)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
