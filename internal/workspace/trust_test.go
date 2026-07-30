package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitRoot(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	sub := filepath.Join(root, "pkg", "api")
	mustMkdir(t, sub)

	if got := GitRoot(sub); got != root {
		t.Fatalf("GitRoot(sub) = %q, want %q", got, root)
	}
	if got := GitRoot(root); got != root {
		t.Fatalf("GitRoot(root) = %q, want %q", got, root)
	}
	if got := GitRoot(t.TempDir()); got != "" {
		t.Fatalf("GitRoot(no git) = %q, want empty", got)
	}
}

func TestTrustTargetUsesGitRoot(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	sub := filepath.Join(root, "nested")
	mustMkdir(t, sub)

	if got := TrustTarget(sub); got != root {
		t.Fatalf("TrustTarget(sub) = %q, want %q", got, root)
	}
}

func TestTrustTargetNoGitUsesCwd(t *testing.T) {
	dir := t.TempDir()
	if got := TrustTarget(dir); got != filepath.Clean(dir) {
		t.Fatalf("TrustTarget = %q, want %q", got, dir)
	}
}

func TestTrustPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)

	dir := t.TempDir()
	if IsTrusted(dir) {
		t.Fatal("expected untrusted")
	}
	if err := Trust(dir); err != nil {
		t.Fatal(err)
	}
	if !IsTrusted(dir) {
		t.Fatal("expected trusted after Trust")
	}
	// Idempotent.
	if err := Trust(dir); err != nil {
		t.Fatal(err)
	}
	store, err := loadTrusted()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Paths) != 1 {
		t.Fatalf("paths = %v, want one entry", store.Paths)
	}
}

func TestTrustNormalizesToGitRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	sub := filepath.Join(root, "src")
	mustMkdir(t, sub)

	// Trust(subdir) must record the git root, not the subdir.
	if err := Trust(sub); err != nil {
		t.Fatal(err)
	}
	if !IsTrusted(sub) {
		t.Fatal("subdir should be trusted via git-root normalization")
	}
	if !IsTrusted(root) {
		t.Fatal("git root should be trusted")
	}
	store, err := loadTrusted()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Paths) != 1 || !samePath(store.Paths[0], root) {
		t.Fatalf("store = %v, want [%q]", store.Paths, root)
	}
}

func TestTrustCoversGitSubdir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	sub := filepath.Join(root, "src")
	mustMkdir(t, sub)

	if err := Trust(root); err != nil {
		t.Fatal(err)
	}
	if !IsTrusted(sub) {
		t.Fatal("subdir should inherit git-root trust")
	}
}

func TestCorruptTrustedJSONTreatedEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	mustWrite(t, filepath.Join(home, trustedFile), "{not json")

	dir := t.TempDir()
	if IsTrusted(dir) {
		t.Fatal("corrupt store must not trust anything")
	}
	// Trust rewrites the allowlist.
	if err := Trust(dir); err != nil {
		t.Fatal(err)
	}
	if !IsTrusted(dir) {
		t.Fatal("expected trusted after rewrite")
	}
}

func TestDisplayPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	if got := DisplayPath(home); got != "~" {
		t.Fatalf("home = %q", got)
	}
	nested := filepath.Join(home, "code", "zeta")
	if got := DisplayPath(nested); got != "~/code/zeta" {
		t.Fatalf("nested = %q", got)
	}
	if got := DisplayPath("/tmp/x"); got != "/tmp/x" {
		t.Fatalf("abs = %q", got)
	}
}
