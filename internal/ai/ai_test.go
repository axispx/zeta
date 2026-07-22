package ai

import "testing"

func TestCleanTitle(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`"Auth Middleware Fix"`, "Auth Middleware Fix"},
		{"Title: Fix the flaky tests\n", "Fix the flaky tests"},
		{"  session: rename helper  ", "Rename helper"},
		{"# Ask Mode Prompt Critique & Suggestions", "Ask Mode Prompt Critique Suggestions"},
		{"Ask Mode Prompt Critique & Suggestions", "Ask Mode Prompt Critique Suggestions"},
		{"\n\nask mode prompt\nextra junk", "Ask mode prompt"},
		{
			"This is a very long session title that should be clipped for the picker UI",
			"This is a very long session title that should be",
		},
		{"flaky auth tests", "Flaky auth tests"},
		{"AI streaming bug", "AI streaming bug"},
	}
	for _, tt := range tests {
		if got := cleanTitle(tt.in); got != tt.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
