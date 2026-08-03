package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

func testFooterCfg() config.Config {
	return config.Config{
		Active: "test/gpt-4",
		Providers: map[string]config.Provider{
			"test": {
				Name:    "Test",
				BaseURL: "http://x",
				APIKey:  "k",
				Models: map[string]config.ModelDef{
					"gpt-4": {Name: "GPT-4", ContextWindow: 100000},
				},
			},
		},
	}
}

func TestInputFooterLayout(t *testing.T) {
	cfg := testFooterCfg()
	ws := workspace.Context{Cwd: "~/proj", Branch: "main"}
	diff := lineStats{added: 12, deleted: 3}
	out := inputFooter(80, ws, cfg, prompt.ModePlan, 18000, diff)
	plain := stripANSI(out)
	lines := strings.Split(plain, "\n")
	if len(lines) != footerRows {
		t.Fatalf("footer rows = %d, want %d:\n%s", len(lines), footerRows, plain)
	}
	// Row 0: model · % · tokens left, mode right.
	if !strings.HasPrefix(strings.TrimLeft(lines[0], " "), "Test GPT-4") {
		t.Fatalf("top should start with model: %q", lines[0])
	}
	if !strings.Contains(lines[0], "18.0k") || !strings.Contains(lines[0], "18%") {
		t.Fatalf("top missing usage: %q", lines[0])
	}
	if !strings.Contains(lines[0], "Test GPT-4") {
		t.Fatalf("top missing model: %q", lines[0])
	}
	if !strings.HasSuffix(strings.TrimRight(lines[0], " "), "Plan") {
		t.Fatalf("top should end with mode: %q", lines[0])
	}
	if strings.Contains(lines[0], "proj") || strings.Contains(lines[0], "+12") {
		t.Fatalf("top should be usage/model/mode only: %q", lines[0])
	}
	// model · % · tokens on the left.
	mi, pi, ti := strings.Index(lines[0], "Test GPT-4"), strings.Index(lines[0], "18%"), strings.Index(lines[0], "18.0k")
	if mi < 0 || pi < 0 || ti < 0 || !(mi < pi && pi < ti) {
		t.Fatalf("want model then %% then tokens: %q", lines[0])
	}
	// Row 1: path left, diff stats right.
	if !strings.Contains(lines[1], "proj") {
		t.Fatalf("bottom missing cwd: %q", lines[1])
	}
	if !strings.Contains(lines[1], "main") {
		t.Fatalf("bottom missing branch: %q", lines[1])
	}
	if !strings.Contains(lines[1], "+12") || !strings.Contains(lines[1], "-3") {
		t.Fatalf("bottom missing diff stats: %q", lines[1])
	}
	if strings.Contains(lines[1], "18.0k") || strings.Contains(lines[1], "GPT-4") {
		t.Fatalf("bottom should be path/stats only: %q", lines[1])
	}
	if i, j := strings.Index(lines[1], "proj"), strings.Index(lines[1], "+12"); i < 0 || j < 0 || i > j {
		t.Fatalf("path should precede stats: %q", lines[1])
	}
}

func TestInputFooterHidesEmptyDiff(t *testing.T) {
	cfg := testFooterCfg()
	ws := workspace.Context{Cwd: "~/proj"}
	out := stripANSI(inputFooter(80, ws, cfg, prompt.ModeBuild, 0, lineStats{}))
	if strings.Contains(out, "+0") || strings.Contains(out, "-0") {
		t.Fatalf("empty diff should be omitted: %q", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != footerRows {
		t.Fatalf("rows = %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "Build") {
		t.Fatalf("top missing mode: %q", lines[0])
	}
	if !strings.Contains(lines[0], "Test GPT-4") {
		t.Fatalf("top missing model when no usage: %q", lines[0])
	}
	if !strings.Contains(lines[1], "proj") {
		t.Fatalf("bottom missing path: %q", lines[1])
	}
}

func TestFormatDiffStats(t *testing.T) {
	if got := formatDiffStats(lineStats{}); got != "" {
		t.Fatalf("empty: %q", got)
	}
	got := stripANSI(formatDiffStats(lineStats{added: 4, deleted: 1}))
	if got != "+4 -1" {
		t.Fatalf("got %q", got)
	}
	if got := stripANSI(formatDiffStats(lineStats{added: 3})); got != "+3" {
		t.Fatalf("adds only: %q", got)
	}
	if got := stripANSI(formatDiffStats(lineStats{deleted: 2})); got != "-2" {
		t.Fatalf("dels only: %q", got)
	}
}

func TestSessionDiff(t *testing.T) {
	diff := "--- a\n+++ b\n@@ -1 +1,2 @@\n-old\n+new\n+extra\n"
	writeOut := "--- a\n+++ b\n+only\n"
	msgs := []Message{
		{Role: RoleTool, Tool: tools.Edit, Status: ToolOK, Out: diff},                        // +2 -1
		{Role: RoleTool, Tool: tools.Write, Status: ToolOK, Out: writeOut},                   // +1
		{Role: RoleTool, Tool: tools.Edit, Status: ToolDenied, Out: "--- a\n+++ b\n+nope\n"}, // ignored
		{Role: RoleTool, Tool: tools.Bash, Status: ToolOK, Out: "hi"},
		{Role: RoleUser, Text: "hi"},
	}
	got := sessionDiff(msgs)
	if got.added != 3 || got.deleted != 1 {
		t.Fatalf("sessionDiff = %+v, want +3 -1", got)
	}

	m := testModel()
	m.messages = msgs
	m.refreshSessionDiff()
	if m.sessionDiff.added != 3 || m.sessionDiff.deleted != 1 {
		t.Fatalf("refreshSessionDiff = %+v, want +3 -1", m.sessionDiff)
	}

	// Live tool finish path: Out set then refresh (same as handleTurnTool).
	m.messages = []Message{
		{Role: RoleTool, Tool: tools.Edit, Status: ToolOK, Out: diff},
	}
	m.refreshSessionDiff()
	if m.sessionDiff.added != 2 || m.sessionDiff.deleted != 1 {
		t.Fatalf("after edit: %+v, want +2 -1", m.sessionDiff)
	}
	m.messages = append(m.messages, Message{Role: RoleTool, Tool: tools.Write, Status: ToolOK, Out: writeOut})
	m.refreshSessionDiff()
	if m.sessionDiff.added != 3 || m.sessionDiff.deleted != 1 {
		t.Fatalf("after write: %+v, want +3 -1", m.sessionDiff)
	}
	// Non-edit tool does not change totals when rescanned.
	m.messages = append(m.messages, Message{Role: RoleTool, Tool: tools.Bash, Status: ToolOK, Out: "hi"})
	m.refreshSessionDiff()
	if m.sessionDiff.added != 3 || m.sessionDiff.deleted != 1 {
		t.Fatalf("after bash: %+v, want +3 -1", m.sessionDiff)
	}

	m.applySession(nil, nil, nil)
	if !m.sessionDiff.empty() {
		t.Fatalf("new session should zero diff: %+v", m.sessionDiff)
	}
}

func TestFooterPathLabel(t *testing.T) {
	// Full label fits.
	if got := footerPathLabel("~/proj", "main", 80); got != "~/proj · main" {
		t.Fatalf("full: %q", got)
	}
	// Drop leading path segments, keep branch.
	long := "~/Developer/axispx/deep/nested/project"
	got := footerPathLabel(long, "main", 22) // "…/project · main" = 16
	if !strings.Contains(got, "project") || !strings.Contains(got, "main") {
		t.Fatalf("shortened path should keep leaf+branch: %q", got)
	}
	if strings.Contains(got, "Developer") {
		t.Fatalf("should drop leading dirs: %q", got)
	}
	if w := lipgloss.Width(got); w > 22 {
		t.Fatalf("width %d > 22: %q", w, got)
	}
	// Very tight: branch alone.
	if got := footerPathLabel(long, "main", 4); got != "main" {
		t.Fatalf("branch alone: %q", got)
	}
	// No branch: path shorten only.
	if got := footerPathLabel("~/a/b/c", "", 6); got != "…/c" && got != "…/b/c" {
		// 6 cells: "…/c" fits
		if lipgloss.Width(got) > 6 || !strings.HasSuffix(got, "c") {
			t.Fatalf("path only: %q", got)
		}
	}
}

func TestShortenPath(t *testing.T) {
	if got := shortenPath("~/a/b/c", 80); got != "~/a/b/c" {
		t.Fatalf("fit: %q", got)
	}
	// "…/c" is 3 cells; with more room prefer more tail segments.
	if got := shortenPath("~/a/b/c", 3); got != "…/c" {
		t.Fatalf("leaf: %q", got)
	}
	if got := shortenPath("~/a/b/c", 5); got != "…/b/c" {
		t.Fatalf("two segs: %q", got)
	}
	if got := shortenPath("verylongname", 6); got != "…gname" {
		t.Fatalf("hard left: %q", got)
	}
}

func TestFooterBottomRespectsWidth(t *testing.T) {
	ws := workspace.Context{
		Cwd:    "~/Developer/axispx/very/deep/project",
		Branch: "feature/long-name",
	}
	out := stripANSI(footerBottomRow(30, ws, lineStats{added: 12, deleted: 3}))
	if w := lipgloss.Width(out); w > 30 {
		t.Fatalf("bottom width %d > 30: %q", w, out)
	}
	// Diff stats should still show when path is shortened.
	if !strings.Contains(out, "+12") || !strings.Contains(out, "-3") {
		t.Fatalf("should keep diff stats: %q", out)
	}
}

func TestFormatUsage(t *testing.T) {
	if got := formatUsage(0, 1000); got != "" {
		t.Fatalf("empty when no tokens: %q", got)
	}
	if got := formatUsage(500, 0); got != "500" {
		t.Fatalf("no pct without window: %q", got)
	}
	if got := formatUsage(25000, 100000); got != "25% · 25.0k" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{42, "42"},
		{1500, "1.5k"},
		{12300, "12.3k"},
		{1_500_000, "1.5M"},
	}
	for _, tt := range tests {
		if got := formatTokenCount(tt.n); got != tt.want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestModeStyle(t *testing.T) {
	tests := []struct {
		mode  prompt.Mode
		style lipgloss.Style
	}{
		{prompt.ModeBuild, styles.StyleModeBuild},
		{prompt.ModeAsk, styles.StyleModeAsk},
		{prompt.ModePlan, styles.StyleModePlan},
	}
	for _, tt := range tests {
		if got, want := modeStyle(tt.mode).GetForeground(), tt.style.GetForeground(); got != want {
			t.Errorf("modeStyle(%v) = %v, want %v", tt.mode, got, want)
		}
	}
}
