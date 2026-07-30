package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/workspace"
)

func TestEnsureTrustedNoopWhenTrusted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)

	dir := t.TempDir()
	t.Chdir(dir)
	if err := workspace.Trust(dir); err != nil {
		t.Fatal(err)
	}
	if err := ensureTrusted(strings.NewReader(""), &strings.Builder{}); err != nil {
		t.Fatalf("EnsureTrusted: %v", err)
	}
}

func TestEnsureTrustedAccept(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)

	dir := t.TempDir()
	t.Chdir(dir)

	var out strings.Builder
	if err := ensureTrusted(strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if !workspace.IsTrusted(dir) {
		t.Fatal("directory not marked trusted")
	}
	got := out.String()
	if !strings.Contains(got, "Trust this folder?") {
		t.Fatalf("missing prompt: %q", got)
	}
}

func TestEnsureTrustedAcceptYes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	dir := t.TempDir()
	t.Chdir(dir)

	if err := ensureTrusted(strings.NewReader("YES\n"), &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if !workspace.IsTrusted(dir) {
		t.Fatal("expected trusted")
	}
}

func TestEnsureTrustedDecline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	dir := t.TempDir()
	t.Chdir(dir)

	for _, ans := range []string{"n\n", "\n", "no\n", "maybe\n"} {
		t.Run(strings.TrimSpace(ans), func(t *testing.T) {
			// Fresh home per case so prior accepts don't leak.
			t.Setenv("ZETA_HOME", t.TempDir())
			err := ensureTrusted(strings.NewReader(ans), &strings.Builder{})
			if !errors.Is(err, ErrTrustDeclined) {
				t.Fatalf("got %v, want ErrTrustDeclined", err)
			}
			if workspace.IsTrusted(dir) {
				t.Fatal("must not trust on decline")
			}
		})
	}
}

func TestEnsureTrustedEOFDeclines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	dir := t.TempDir()
	t.Chdir(dir)

	err := ensureTrusted(strings.NewReader(""), &strings.Builder{})
	if !errors.Is(err, ErrTrustDeclined) {
		t.Fatalf("got %v, want ErrTrustDeclined", err)
	}
}

func TestEnsureTrustedGitRootNote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	var out strings.Builder
	if err := ensureTrusted(strings.NewReader("y\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !workspace.IsTrusted(root) {
		t.Fatal("git root should be trusted")
	}
	if !strings.Contains(out.String(), "repository root") {
		t.Fatalf("expected git note, got %q", out.String())
	}
}

func TestEnsureTrustedNonInteractive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	dir := t.TempDir()
	t.Chdir(dir)

	// A real file is a non-char-device *os.File → non-interactive.
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	var out strings.Builder
	err = ensureTrusted(f, &out)
	if !errors.Is(err, ErrTrustDeclined) {
		t.Fatalf("got %v, want ErrTrustDeclined", err)
	}
	if !strings.Contains(out.String(), "not trusted") {
		t.Fatalf("expected message, got %q", out.String())
	}
	if workspace.IsTrusted(dir) {
		t.Fatal("must not auto-trust non-interactive")
	}
}

func TestEnsureTrustedPersistError(t *testing.T) {
	// ZETA_HOME as a file makes EnsureHome/write fail.
	fileHome := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(fileHome, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZETA_HOME", fileHome)
	t.Chdir(t.TempDir())

	err := ensureTrusted(strings.NewReader("y\n"), &strings.Builder{})
	if err == nil || errors.Is(err, ErrTrustDeclined) {
		t.Fatalf("want persist error, got %v", err)
	}
}
