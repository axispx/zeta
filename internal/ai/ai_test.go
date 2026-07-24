package ai

import "testing"

func TestReasoningFromRaw(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"content only", `{"content":"hi"}`, ""},
		{"no reasoning key", `{"content":"hi","role":"assistant"}`, ""},
		{"reasoning_content", `{"reasoning_content":"think"}`, "think"},
		{"reasoning", `{"reasoning":"ponder"}`, "ponder"},
		{"prefers reasoning_content", `{"reasoning_content":"a","reasoning":"b"}`, "a"},
		{"invalid", `{`, ""},
		{"nullish", `{"reasoning_content":null}`, ""},
		// fast-path: substring "reasoning" without a real field still unmarshals empty
		{"false positive substring", `{"note":"reasoning about x"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reasoningFromRaw(tt.raw); got != tt.want {
				t.Fatalf("reasoningFromRaw(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

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
