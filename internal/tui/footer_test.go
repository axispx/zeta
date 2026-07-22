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
		Model: "test/gpt-4",
		Providers: []config.Provider{
			{
				ID:      "test",
				Name:    "Test",
				BaseURL: "http://x",
				APIKey:  "k",
				Models: map[string]config.ModelDef{
					"gpt-4": {Name: "GPT-4"},
				},
			},
		},
	}
	ws := workspace.Context{Cwd: "~/proj", Branch: "main"}
	out := inputFooter(80, ws, cfg, prompt.ModePlan)
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
