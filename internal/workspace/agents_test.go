package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNearestAgentsPrefersNested(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "root agents")
	sub := filepath.Join(root, "pkg", "api")
	mustMkdir(t, sub)
	mustWrite(t, filepath.Join(sub, "AGENTS.md"), "nested agents")

	if agents := NearestAgents(sub, root); agents != "nested agents" {
		t.Fatalf("NearestAgents(sub) = %q, want nested agents", agents)
	}
}

func TestNearestAgentsWalksToCeiling(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "root agents")
	sub := filepath.Join(root, "pkg", "api")
	mustMkdir(t, sub)

	if agents := NearestAgents(sub, TrustTarget(sub)); agents != "root agents" {
		t.Fatalf("NearestAgents(sub) = %q, want root agents", agents)
	}
	if branch := Branch(sub); branch != "main" {
		t.Fatalf("Branch(sub) = %q, want main", branch)
	}
}

func TestNearestAgentsStopsAtGitRoot(t *testing.T) {
	outer := t.TempDir()
	mustWrite(t, filepath.Join(outer, "AGENTS.md"), "outer agents")
	root := filepath.Join(outer, "repo")
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/dev\n")
	sub := filepath.Join(root, "src")
	mustMkdir(t, sub)

	if agents := NearestAgents(sub, TrustTarget(sub)); agents != "" {
		t.Fatalf("NearestAgents(sub) = %q, want empty (should not escape git root)", agents)
	}
	if branch := Branch(sub); branch != "dev" {
		t.Fatalf("Branch(sub) = %q, want dev", branch)
	}
}

func TestNearestAgentsNoGitStaysInCwd(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "parent agents")
	nested := filepath.Join(dir, "nested")
	mustMkdir(t, nested)
	mustWrite(t, filepath.Join(nested, "AGENTS.md"), "nested agents")

	if agents := NearestAgents(dir, TrustTarget(dir)); agents != "parent agents" {
		t.Fatalf("NearestAgents(dir) = %q, want parent agents", agents)
	}
	// Trust target without git is cwd — do not walk into parents.
	if agents := NearestAgents(nested, TrustTarget(nested)); agents != "nested agents" {
		t.Fatalf("NearestAgents(nested) = %q, want nested only (no parent walk)", agents)
	}
	if branch := Branch(nested); branch != "" {
		t.Fatalf("Branch(nested) = %q, want empty", branch)
	}

	emptyNested := filepath.Join(dir, "empty")
	mustMkdir(t, emptyNested)
	if agents := NearestAgents(emptyNested, TrustTarget(emptyNested)); agents != "" {
		t.Fatalf("NearestAgents(emptyNested) = %q, want empty (parent must not load)", agents)
	}
}

func TestLoadOmitsAgentsWhenUntrusted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "inject me")
	t.Chdir(dir)

	c := Load()
	if c.AgentsMD != "" {
		t.Fatalf("untrusted Load AgentsMD = %q, want empty", c.AgentsMD)
	}
	if c.Abs == "" {
		t.Fatal("expected Abs")
	}

	if err := Trust(dir); err != nil {
		t.Fatal(err)
	}
	c = Load()
	if c.AgentsMD != "inject me" {
		t.Fatalf("trusted Load AgentsMD = %q, want inject me", c.AgentsMD)
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

func TestNearestAgentsEmptyIgnored(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "  \n  ")
	if agents := NearestAgents(dir, dir); agents != "" {
		t.Fatalf("NearestAgents = %q, want empty for whitespace-only file", agents)
	}
}

func TestNearestAgentsTruncates(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("a", maxAgentsMD+100)
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), big)

	agents := NearestAgents(dir, dir)
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
