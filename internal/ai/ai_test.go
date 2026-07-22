package ai

import "testing"

func TestCleanTitle(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`"Auth Middleware Fix"`, "Auth Middleware Fix"},
		{"Title: Fix the flaky tests\n", "Fix the flaky tests"},
		{"  session: rename helper  ", "rename helper"},
	}
	for _, tt := range tests {
		if got := cleanTitle(tt.in); got != tt.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
