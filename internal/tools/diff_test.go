package tools

import (
	"strings"
	"testing"
)

func TestUnifiedDiff(t *testing.T) {
	got := unifiedDiff("a.txt", "one\ntwo\n", "one\nTWO\n")
	if !strings.Contains(got, "--- a.txt") || !strings.Contains(got, "+++ a.txt") {
		t.Fatalf("headers: %q", got)
	}
	if !strings.Contains(got, "-two") || !strings.Contains(got, "+TWO") {
		t.Fatalf("hunk: %q", got)
	}
	if unifiedDiff("x", "same", "same") != "" {
		t.Fatal("identical should be empty")
	}
}

func TestTruncateDiff(t *testing.T) {
	if got := truncateDiff("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
	var b strings.Builder
	for b.Len() < maxDiffBytes+100 {
		b.WriteString("+line of content that pads the diff output\n")
	}
	got := truncateDiff(b.String())
	if len(got) >= len(b.String()) {
		t.Fatalf("expected truncation: in=%d out=%d", len(b.String()), len(got))
	}
	if !strings.Contains(got, "[truncated:") {
		t.Fatalf("missing trunc note: %q", got[max(0, len(got)-80):])
	}
}

func TestEditReturnsDiff(t *testing.T) {
	root := t.TempDir()
	ctx := t.Context()
	ts := Build()

	out := Run(ctx, ts, root, "edit", mustRaw(t, map[string]any{
		"path": "hello.txt", "old_string": "", "new_string": "one\ntwo\n",
	}))
	if !strings.Contains(out, "--- hello.txt") || !strings.Contains(out, "+++ hello.txt") {
		t.Fatalf("create headers: %s", out)
	}
	if !strings.Contains(out, "+one") || !strings.Contains(out, "+two") {
		t.Fatalf("create diff: %s", out)
	}
	if strings.Contains(out, "created ") {
		t.Fatalf("create should return diff only: %s", out)
	}

	out = Run(ctx, ts, root, "edit", mustRaw(t, map[string]any{
		"path": "hello.txt", "old_string": "two", "new_string": "TWO",
	}))
	if !strings.Contains(out, "-two") || !strings.Contains(out, "+TWO") {
		t.Fatalf("edit diff: %s", out)
	}
	if strings.HasPrefix(out, "edited ") {
		t.Fatalf("edit should return diff only: %s", out)
	}

	// Empty create and no-op replace return empty (never prose).
	out = Run(ctx, ts, root, "edit", mustRaw(t, map[string]any{
		"path": "empty.txt", "old_string": "", "new_string": "",
	}))
	if out != "" {
		t.Fatalf("empty create: %q", out)
	}
	out = Run(ctx, ts, root, "edit", mustRaw(t, map[string]any{
		"path": "hello.txt", "old_string": "TWO", "new_string": "TWO",
	}))
	if out != "" {
		t.Fatalf("noop edit: %q", out)
	}
}

func TestEditSummaryCreateVsEdit(t *testing.T) {
	var e editTool
	if got := e.Summary(mustRaw(t, map[string]any{"path": "a.go", "old_string": "", "new_string": "x"})); got != "create a.go" {
		t.Fatalf("create summary: %q", got)
	}
	if got := e.Summary(mustRaw(t, map[string]any{"path": "a.go", "old_string": "a", "new_string": "b"})); got != "edit a.go" {
		t.Fatalf("edit summary: %q", got)
	}
}
