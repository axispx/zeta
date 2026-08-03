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

func TestLargeDiffNotLimitedViaRun(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	root := t.TempDir()
	// Build a large replace so the edit tool returns a big unified diff.
	var old, neu strings.Builder
	for i := 0; i < 3000; i++ {
		old.WriteString("line of original content that is long enough pad\n")
		neu.WriteString("line of changed content that is long enough pad\n")
	}
	// Write old via create then replace whole file.
	_ = Run(t.Context(), Build(), root, Edit, mustRaw(t, map[string]any{
		"path": "big.txt", "old_string": "", "new_string": old.String(),
	}))
	out := Run(t.Context(), Build(), root, Edit, mustRaw(t, map[string]any{
		"path": "big.txt", "old_string": old.String(), "new_string": neu.String(),
	}))
	if strings.HasPrefix(out, "error:") {
		t.Fatal(out)
	}
	// Full patch for review — not head+tail truncated like bash/read/etc.
	if strings.Contains(out, "[truncated:") {
		t.Fatalf("edit diff must not be truncated, len=%d", len(out))
	}
	if got := strings.Count(out, "\n+line of changed"); got < 3000 {
		t.Fatalf("want full +lines, got %d (len=%d)", got, len(out))
	}
	if got := strings.Count(out, "\n-line of original"); got < 3000 {
		t.Fatalf("want full -lines, got %d (len=%d)", got, len(out))
	}
}

func TestEditReturnsDiff(t *testing.T) {
	root := t.TempDir()
	ctx := t.Context()
	ts := Build()

	out := Run(ctx, ts, root, Edit, mustRaw(t, map[string]any{
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

	out = Run(ctx, ts, root, Edit, mustRaw(t, map[string]any{
		"path": "hello.txt", "old_string": "two", "new_string": "TWO",
	}))
	if !strings.Contains(out, "-two") || !strings.Contains(out, "+TWO") {
		t.Fatalf("edit diff: %s", out)
	}
	if strings.HasPrefix(out, "edited ") {
		t.Fatalf("edit should return diff only: %s", out)
	}

	// Empty create and no-op replace return empty (never prose).
	out = Run(ctx, ts, root, Edit, mustRaw(t, map[string]any{
		"path": "empty.txt", "old_string": "", "new_string": "",
	}))
	if out != "" {
		t.Fatalf("empty create: %q", out)
	}
	out = Run(ctx, ts, root, Edit, mustRaw(t, map[string]any{
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
