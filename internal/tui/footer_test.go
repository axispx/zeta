package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/workspace"
)

func TestInputFooterShowsMode(t *testing.T) {
	cfg := config.Config{
		Active: "test/gpt-4",
		Providers: map[string]config.Provider{
			"test": {
				Name:    "Test",
				BaseURL: "http://x",
				APIKey:  "k",
				Models: map[string]config.ModelDef{
					"gpt-4": {Name: "GPT-4", ContextWindow: 128000},
				},
			},
		},
	}
	ws := workspace.Context{Cwd: "~/proj", Branch: "main"}
	out := inputFooter(80, ws, cfg, prompt.ModePlan, 0)
	if !strings.Contains(out, "Plan") {
		t.Fatalf("footer missing mode: %q", out)
	}
	if !strings.Contains(out, "Test GPT-4") {
		t.Fatalf("footer missing model: %q", out)
	}
	if !strings.Contains(out, "proj") {
		t.Fatalf("footer missing cwd: %q", out)
	}
}

func TestInputFooterShowsUsage(t *testing.T) {
	cfg := config.Config{
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
	ws := workspace.Context{Cwd: "~/proj"}
	out := inputFooter(100, ws, cfg, prompt.ModeBuild, 18000)
	if !strings.Contains(out, "18.0k") {
		t.Fatalf("footer missing tokens: %q", out)
	}
	if !strings.Contains(out, "18%") {
		t.Fatalf("footer missing context %%: %q", out)
	}
}

func TestFormatUsage(t *testing.T) {
	if got := formatUsage(0, 1000); got != "" {
		t.Fatalf("empty when no tokens: %q", got)
	}
	if got := formatUsage(500, 0); got != "500" {
		t.Fatalf("no pct without window: %q", got)
	}
	if got := formatUsage(25000, 100000); got != "25.0k · 25%" {
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
