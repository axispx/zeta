package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/config"
)

func newDropModel(t *testing.T) Model {
	t.Helper()
	isolateZetaHome(t)
	m, err := New(config.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.ready = true
	m.width, m.height = 80, 24
	m.layout()
	return m
}

func TestSplitDropTokens(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"/a/b.txt", []string{"/a/b.txt"}},
		{"'/a/My Dir/b.txt'", []string{"'/a/My Dir/b.txt'"}},
		{`/a/My\ Dir/b.txt`, []string{`/a/My\ Dir/b.txt`}},
		{"/a/one.txt /a/two.txt", []string{"/a/one.txt", "/a/two.txt"}},
		{"file:///a/one.txt\nfile:///a/two.txt\n", []string{"file:///a/one.txt", "file:///a/two.txt"}},
		{"# comment\n/a/one.txt", []string{"/a/one.txt"}},
		{"'/a/unterminated", nil},
		{"", nil},
	}
	for _, c := range cases {
		got := splitDropTokens(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitDropTokens(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestDropPathsOnlyExistingFiles(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "one.txt")
	two := filepath.Join(dir, "two.txt")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if got := dropPaths(one); len(got) != 1 || got[0] != one {
		t.Fatalf("single=%q", got)
	}
	if got := dropPaths("'" + one + "' " + two); len(got) != 2 || got[0] != one || got[1] != two {
		t.Fatalf("quoted pair=%q", got)
	}
	if got := dropPaths("file://" + one); len(got) != 1 || got[0] != one {
		t.Fatalf("uri=%q", got)
	}
	// Plain text and non-existent paths keep normal paste behavior.
	for _, in := range []string{"hello world", filepath.Join(dir, "missing.txt"), strings.Repeat("x", 10) + " " + one} {
		if got := dropPaths(in); got != nil {
			t.Errorf("dropPaths(%q)=%q want nil", in, got)
		}
	}
}

func TestDropInsertSpacing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("empty prompt", func(t *testing.T) {
		m := newDropModel(t)
		if !m.handleBracketPaste(file) {
			t.Fatal("expected drop to be consumed")
		}
		if got, want := m.textarea.Value(), file+" "; got != want {
			t.Fatalf("value=%q want %q", got, want)
		}
	})

	t.Run("text before cursor", func(t *testing.T) {
		m := newDropModel(t)
		m.textarea.SetValue("review this")
		m.textarea.MoveToEnd()
		if !m.handleBracketPaste(file) {
			t.Fatal("expected drop to be consumed")
		}
		if got, want := m.textarea.Value(), "review this "+file+" "; got != want {
			t.Fatalf("value=%q want %q", got, want)
		}
	})

	t.Run("existing trailing space is kept single", func(t *testing.T) {
		m := newDropModel(t)
		m.textarea.SetValue("look at ")
		m.textarea.MoveToEnd()
		m.handleBracketPaste(file)
		if got, want := m.textarea.Value(), "look at "+file+" "; got != want {
			t.Fatalf("value=%q want %q", got, want)
		}
	})

	t.Run("drop at start of text needs no leading space", func(t *testing.T) {
		m := newDropModel(t)
		m.textarea.SetValue("tail")
		m.textarea.MoveToBegin()
		m.handleBracketPaste(file)
		if got, want := m.textarea.Value(), file+" tail"; got != want {
			t.Fatalf("value=%q want %q", got, want)
		}
	})

	t.Run("plain text falls through", func(t *testing.T) {
		m := newDropModel(t)
		m.textarea.SetValue("keep")
		m.textarea.MoveToEnd()
		if m.handleBracketPaste("just some words") {
			t.Fatal("plain text must not be consumed")
		}
	})
}

func TestClipboardTextDropInsertsPath(t *testing.T) {
	m := newDropModel(t)
	dir := t.TempDir()
	two := filepath.Join(dir, "two.txt")
	if err := os.WriteFile(two, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.textarea.SetValue("see")
	m.textarea.MoveToEnd()
	m.insertDropPaths([]string{two})
	if got, want := m.textarea.Value(), "see "+two+" "; got != want {
		t.Fatalf("value=%q want %q", got, want)
	}
}
