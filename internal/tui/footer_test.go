package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
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
	plain := stripANSI(inputFooter(80, ws, cfg, 18000))
	lines := strings.Split(plain, "\n")
	if len(lines) != footerRows {
		t.Fatalf("footer rows = %d, want %d:\n%s", len(lines), footerRows, plain)
	}
	// Row 0: path · branch left, model right-aligned.
	if !strings.HasPrefix(lines[0], "~/proj · main") {
		t.Fatalf("top should start with path · branch: %q", lines[0])
	}
	if !strings.HasSuffix(lines[0], "Test GPT-4 · 18%") || lipgloss.Width(lines[0]) != 80 {
		t.Fatalf("top should end with model at full width: %q", lines[0])
	}
}

func TestInputFooterShowsReasoningEffort(t *testing.T) {
	cfg := testFooterCfg()
	md := cfg.Providers["test"].Models["gpt-4"]
	md.ReasoningEffort = "high"
	cfg.Providers["test"].Models["gpt-4"] = md
	out := stripANSI(inputFooter(80, workspace.Context{Cwd: "~/proj"}, cfg, 0))
	lines := strings.Split(out, "\n")
	if !strings.HasSuffix(lines[0], "Test GPT-4 High") {
		t.Fatalf("top missing model/effort: %q", lines[0])
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
	out := stripANSI(footerPathLabel(ws.Cwd, ws.Branch, 30))
	if w := lipgloss.Width(out); w > 30 {
		t.Fatalf("bottom width %d > 30: %q", w, out)
	}
}

func TestFormatUsage(t *testing.T) {
	if got := formatUsage(0, 1000); got != "" {
		t.Fatalf("empty when no tokens: %q", got)
	}
	if got := formatUsage(500, 0); got != "" {
		t.Fatalf("empty when no window: %q", got)
	}
	if got := formatUsage(25000, 100000); got != "25%" {
		t.Fatalf("got %q", got)
	}
	// Sub-1% fill still shows as 1% rather than 0%.
	if got := formatUsage(10, 100000); got != "1%" {
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
