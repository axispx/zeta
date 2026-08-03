package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
	out := Run(ctx, ts, root, Edit, create)
	if !strings.Contains(out, "--- hello.txt") || !strings.Contains(out, "+one") {
		t.Fatalf("create: %s", out)
	}

	readOut := Run(ctx, ts, root, Read, mustRaw(t, map[string]any{"path": "hello.txt"}))
	if !strings.Contains(readOut, "one") || !strings.Contains(readOut, "1|") {
		t.Fatalf("read: %s", readOut)
	}

	edit := mustRaw(t, map[string]any{
		"path": "hello.txt", "old_string": "two", "new_string": "TWO",
	})
	out = Run(ctx, ts, root, Edit, edit)
	if !strings.Contains(out, "-two") || !strings.Contains(out, "+TWO") {
		t.Fatalf("edit: %s", out)
	}
	data, _ := os.ReadFile(filepath.Join(root, "hello.txt"))
	if !strings.Contains(string(data), "TWO") {
		t.Fatalf("file content: %s", data)
	}

	// ambiguous without replace_all
	_ = os.WriteFile(filepath.Join(root, "dup.txt"), []byte("a a a"), 0o644)
	out = Run(ctx, ts, root, Edit, mustRaw(t, map[string]any{
		"path": "dup.txt", "old_string": "a", "new_string": "b",
	}))
	if !strings.Contains(out, "error:") {
		t.Fatalf("expected uniqueness error, got %s", out)
	}
}

func TestInteractive(t *testing.T) {
	if !Interactive(AskUser) {
		t.Fatal("ask_user must be interactive")
	}
	for _, name := range []string{Read, Bash, Edit, Write, Skill, Todo} {
		if Interactive(name) {
			t.Fatalf("%s must not be interactive", name)
		}
	}
}

func TestSkillToolRun(t *testing.T) {
	root := t.TempDir()
	out := Run(context.Background(), Build(), root, Skill, mustRaw(t, map[string]any{
		"name": "definitely-not-a-real-skill-name-zzz",
	}))
	if !strings.Contains(out, "unknown skill") {
		t.Fatalf("got %s", out)
	}
	out = Run(context.Background(), Build(), root, Skill, mustRaw(t, map[string]any{
		"name": "review",
	}))
	if strings.HasPrefix(out, "error:") || !strings.Contains(out, "Thermo-Nuclear") {
		t.Fatalf("got %s", out[:min(200, len(out))])
	}
	// Available in inspect too.
	out = Run(context.Background(), Inspect(), root, Skill, mustRaw(t, map[string]any{
		"name": "review",
	}))
	if strings.HasPrefix(out, "error:") || !strings.Contains(out, `<skill_content name="review">`) {
		t.Fatalf("inspect: %s", out[:min(200, len(out))])
	}
}

func TestSkillToolSummary(t *testing.T) {
	if got := (skillTool{}).Summary(mustRaw(t, map[string]any{"name": "demo"})); got != "skill demo" {
		t.Fatal(got)
	}
	if got := (skillTool{}).Summary(mustRaw(t, map[string]any{})); got != "skill" {
		t.Fatal(got)
	}
}

func TestInspect(t *testing.T) {
	ro := Inspect()
	if len(ro) != 8 {
		t.Fatalf("inspect len: %d", len(ro))
	}
	if len(Build()) != 11 {
		t.Fatalf("build len: %d", len(Build()))
	}
	names := map[string]bool{}
	for _, tool := range ro {
		names[tool.Name()] = true
	}
	if names[Bash] || names[Edit] || names[Write] || !names[Skill] || !names[Read] || !names[Grep] || !names[Glob] || !names[WebSearch] || !names[WebFetch] || !names[Todo] || !names[AskUser] {
		t.Fatalf("inspect names: %v", names)
	}
}

func TestRunModeGate(t *testing.T) {
	ro := Inspect()
	root := t.TempDir()
	out := Run(context.Background(), ro, root, Edit, mustRaw(t, map[string]any{
		"path": "x", "old_string": "", "new_string": "y",
	}))
	if !strings.Contains(out, "not available in this mode") {
		t.Fatalf("got %s", out)
	}
	out = Run(context.Background(), ro, root, Bash, mustRaw(t, map[string]any{
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
	if got := (editTool{}).Summary(mustRaw(t, map[string]any{"path": "b.go", "old_string": "x"})); got != "edit b.go" {
		t.Fatal(got)
	}
	if got := (editTool{}).Summary(mustRaw(t, map[string]any{"path": "b.go", "old_string": ""})); got != "create b.go" {
		t.Fatal(got)
	}
	if got := (bashTool{}).Summary(mustRaw(t, map[string]any{"command": "go test ./..."})); got != "bash go test ./..." {
		t.Fatal(got)
	}
	long := strings.Repeat("x", 100) + "\nsecond"
	if got := (bashTool{}).Summary(mustRaw(t, map[string]any{"command": long})); got != "bash "+long {
		t.Fatalf("bash should keep full command: %q", got)
	}
}

func TestArgPath(t *testing.T) {
	if got := ArgPath(mustRaw(t, map[string]any{"path": " a/b.go ", "content": "x"})); got != "a/b.go" {
		t.Fatalf("got %q", got)
	}
	if got := ArgPath(mustRaw(t, map[string]any{"command": "echo"})); got != "" {
		t.Fatalf("bash args: %q", got)
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

	out := Run(ctx, ts, root, Read, mustRaw(t, map[string]any{"path": "."}))
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

	out = Run(ctx, ts, root, Read, mustRaw(t, map[string]any{"path": "internal"}))
	if out != "tools/" {
		t.Fatalf("subdir: %q", out)
	}

	out = Run(ctx, ts, root, Read, mustRaw(t, map[string]any{"path": "empty"}))
	if out != "[empty directory]" {
		t.Fatalf("empty: %q", out)
	}

	out = Run(ctx, ts, root, Read, mustRaw(t, map[string]any{
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
	out := Run(ctx, Build(), root, Bash, mustRaw(t, map[string]any{
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

	out := Run(ctx, ts, root, Bash, mustRaw(t, map[string]any{
		"command": "echo hello",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "exit: 0") {
		t.Fatalf("echo: %q", out)
	}

	out = Run(ctx, ts, root, Bash, mustRaw(t, map[string]any{
		"command": "cat note.txt", "workdir": "sub",
	}))
	if !strings.Contains(out, "hi") || !strings.Contains(out, "exit: 0") {
		t.Fatalf("workdir: %q", out)
	}

	out = Run(ctx, ts, root, Bash, mustRaw(t, map[string]any{
		"command": "echo x", "workdir": "../outside",
	}))
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "outside the workspace") {
		t.Fatalf("escape: %q", out)
	}

	out = Run(ctx, ts, root, Bash, mustRaw(t, map[string]any{
		"command": "echo x", "workdir": "missing",
	}))
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "does not exist") {
		t.Fatalf("missing workdir: %q", out)
	}

	out = Run(ctx, ts, root, Bash, mustRaw(t, map[string]any{
		"command": "echo x", "workdir": "sub/note.txt",
	}))
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "not a directory") {
		t.Fatalf("file workdir: %q", out)
	}

	out = Run(ctx, ts, root, Bash, mustRaw(t, map[string]any{
		"command": "exit 7",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "exit: 7") {
		t.Fatalf("nonzero: %q", out)
	}

	out = Run(ctx, ts, root, Bash, mustRaw(t, map[string]any{
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

func TestTruncateLongLines(t *testing.T) {
	if got := truncateLongLines("short"); got != "short" {
		t.Fatalf("short: %q", got)
	}
	if got := truncateLongLines(""); got != "" {
		t.Fatalf("empty: %q", got)
	}

	long := strings.Repeat("a", maxLineBytes+100)
	got := truncateLongLines(long)
	if !strings.HasSuffix(got, maxLineSuffix) {
		t.Fatalf("missing suffix: %q", got[len(got)-40:])
	}
	if !strings.HasPrefix(got, strings.Repeat("a", maxLineBytes)) {
		t.Fatalf("prefix wrong")
	}
	if len(got) != maxLineBytes+len(maxLineSuffix) {
		t.Fatalf("len=%d want %d", len(got), maxLineBytes+len(maxLineSuffix))
	}

	// Multi-line: only long lines are cut.
	multi := long + "\nshort\n" + long
	lines := strings.Split(truncateLongLines(multi), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines: %d", len(lines))
	}
	if lines[1] != "short" {
		t.Fatalf("middle: %q", lines[1])
	}
	if !strings.HasSuffix(lines[0], maxLineSuffix) || !strings.HasSuffix(lines[2], maxLineSuffix) {
		t.Fatalf("ends not truncated: %q / %q", lines[0][len(lines[0])-30:], lines[2][len(lines[2])-30:])
	}

	// Trailing newline preserved.
	if got := truncateLongLines("a\n"); got != "a\n" {
		t.Fatalf("trailing nl: %q", got)
	}

	// UTF-8: do not split a multi-byte rune.
	prefix := strings.Repeat("x", maxLineBytes-1)
	midRune := prefix + "界" + strings.Repeat("y", 50)
	got = cutLine(midRune)
	if !utf8.ValidString(got) {
		t.Fatalf("invalid utf8 after cut")
	}
	if !strings.HasSuffix(got, maxLineSuffix) {
		t.Fatalf("utf8 suffix: %q", got)
	}
}

func TestMiddleTruncate(t *testing.T) {
	// Many short lines → line omit in the middle.
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "LINE-%03d\n", i)
	}
	got := middleTruncate(b.String(), 10*1024, 10)
	if !strings.Contains(got, "lines omitted") {
		t.Fatalf("want line omit: %q", got)
	}
	if !strings.Contains(got, "LINE-000") || !strings.Contains(got, "LINE-099") {
		t.Fatalf("want head and tail: %q", got)
	}
	if strings.Contains(got, "LINE-050") {
		t.Fatalf("middle should be gone: %q", got)
	}

	// Byte budget with head+tail.
	blob := strings.Repeat("A", 200) + "MID" + strings.Repeat("Z", 200)
	got = middleTruncate(blob, 80, 1000)
	if !strings.Contains(got, "bytes omitted") {
		t.Fatalf("want byte omit: %q", got)
	}
	if !strings.HasPrefix(got, "AAAA") || !strings.Contains(got, "ZZZZ") {
		t.Fatalf("want A head and Z tail: %q", got)
	}
	if strings.Contains(got, "MID") {
		t.Fatalf("MID should be omitted: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatal("invalid utf8")
	}
	if len(got) > 80 {
		t.Fatalf("over budget: %d", len(got))
	}
}

func TestLimitToolOutputSpill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)

	var b strings.Builder
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&b, "pad line %05d xxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", i)
	}
	full := b.String()
	if len(full) <= maxToolBytes {
		t.Fatalf("fixture too small: %d", len(full))
	}

	got := limitToolOutput(full)
	if !strings.Contains(got, "[truncated:") {
		t.Fatalf("missing trunc note: %q", got[max(0, len(got)-200):])
	}
	if !strings.Contains(got, "line-capped output") {
		t.Fatalf("want line-capped wording: %q", got[max(0, len(got)-300):])
	}
	if !strings.Contains(got, "saved to") {
		t.Fatalf("missing spill path: %q", got[max(0, len(got)-300):])
	}
	if !strings.Contains(got, "omitted") {
		t.Fatalf("want middle omit marker: %q", got[:min(200, len(got))])
	}
	if len(got) >= len(full) {
		t.Fatalf("preview not smaller: in=%d out=%d", len(full), len(got))
	}
	// Spill file exists and holds line-capped content.
	dir := filepath.Join(home, "cache", "tool-output")
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) == 0 {
		t.Fatalf("spill dir: err=%v ents=%v", err, ents)
	}
	data, err := os.ReadFile(filepath.Join(dir, ents[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < maxToolBytes {
		t.Fatalf("spill too small: %d", len(data))
	}
	// Under budget path unchanged.
	if got := limitToolOutput("tiny"); got != "tiny" {
		t.Fatalf("short: %q", got)
	}
}

func TestSpillPruneOld(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	dir := filepath.Join(home, "cache", "tool-output")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(oldPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-spillMaxAge - time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	path := spillToolOutput(strings.Repeat("x\n", maxToolBytes))
	if path == "" {
		t.Fatal("spill failed")
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old spill should be pruned: err=%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("new spill missing: %v", err)
	}
}

func TestRunTruncatesLongLines(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	root := t.TempDir()
	long := strings.Repeat("z", maxLineBytes+500)
	path := filepath.Join(root, "long.txt")
	if err := os.WriteFile(path, []byte(long+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := Run(context.Background(), Build(), root, Read, mustRaw(t, map[string]any{"path": "long.txt"}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, maxLineSuffix) {
		t.Fatalf("expected line truncation marker in read: %q", out[:min(120, len(out))])
	}

	out = Run(context.Background(), Build(), root, Bash, mustRaw(t, map[string]any{
		"command": "cat long.txt",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, maxLineSuffix) {
		t.Fatalf("expected line truncation in bash: %q", out[:min(120, len(out))])
	}
	if !strings.Contains(out, "exit: 0") {
		t.Fatalf("missing exit: %q", out[max(0, len(out)-60):])
	}
}

func TestRunErrorBypassesLimit(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	root := t.TempDir()
	out := Run(context.Background(), Build(), root, Read, mustRaw(t, map[string]any{"path": "missing-nope.txt"}))
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("want error prefix: %q", out)
	}
	if strings.Contains(out, "saved to") || strings.Contains(out, "omitted") {
		t.Fatalf("errors must not be spilled/limited: %q", out)
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

func TestBashLongLineCapture(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	root := t.TempDir()
	// Continuous stream larger than capture cap (no newlines → line-capped in limitToolOutput).
	out := Run(context.Background(), Build(), root, Bash, mustRaw(t, map[string]any{
		"command": "dd if=/dev/zero bs=1024 count=300 2>/dev/null | tr '\\0' a",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, maxLineSuffix) {
		t.Fatalf("expected line truncation: %q", out[:min(200, len(out))])
	}
	if !strings.Contains(out, "exit: 0") {
		t.Fatalf("exit: %q", out[max(0, len(out)-40):])
	}
	// No dual per-tool capture note.
	if strings.Contains(out, "capture stopped") {
		t.Fatalf("capture notes must stay silent: %q", out[max(0, len(out)-120):])
	}
}

func TestBashManyLinesSpill(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	root := t.TempDir()
	out := Run(context.Background(), Build(), root, Bash, mustRaw(t, map[string]any{
		"command": "i=0; while [ $i -lt 5000 ]; do echo \"log line $i padding padding padding\"; i=$((i+1)); done",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "[truncated:") {
		t.Fatalf("expected trunc footer: len=%d head=%q", len(out), out[:min(120, len(out))])
	}
	if !strings.Contains(out, "omitted") {
		t.Fatalf("expected middle omit: %q", out[:min(200, len(out))])
	}
	if !strings.Contains(out, "saved to") {
		t.Fatalf("expected spill path: %q", out[max(0, len(out)-300):])
	}
	// Tail of model preview should still surface exit status (appended after stdout).
	if !strings.Contains(out, "exit: 0") {
		t.Fatalf("exit: %q", out[max(0, len(out)-80):])
	}
}

func TestGrep(t *testing.T) {
	requireRG(t)
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\nfunc Hello() {}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "b.txt"), []byte("Hello world\n"), 0o644)

	out := Run(context.Background(), Build(), root, Grep, mustRaw(t, map[string]any{
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
	// Relative path output (no absolute root prefix).
	if strings.Contains(out, root) {
		t.Fatalf("expected relative paths, got %q", out)
	}

	out = Run(context.Background(), Build(), root, Grep, mustRaw(t, map[string]any{
		"pattern": "Hello",
		"path":    "a.go",
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "Hello") {
		t.Fatalf("file path search: %q", out)
	}

	out = Run(context.Background(), Build(), root, Grep, mustRaw(t, map[string]any{
		"pattern": "nomatch_xyz",
	}))
	if out != "no matches" {
		t.Fatalf("got %q", out)
	}
}

func TestRgTarget(t *testing.T) {
	root := t.TempDir()
	got, err := rgTarget(root, root)
	if err != nil || got != "" {
		t.Fatalf("root → %q %v", got, err)
	}
	sub := filepath.Join(root, "internal", "tools")
	got, err = rgTarget(root, sub)
	if err != nil || got != filepath.Join("internal", "tools") {
		t.Fatalf("subdir → %q %v", got, err)
	}
	if _, err := rgTarget(root, filepath.Join(root, "..", "elsewhere")); err == nil {
		t.Fatal("expected outside error")
	}
}

func requireRG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
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
