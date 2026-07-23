package tui

import (
	"testing"

	"github.com/axispx/zeta/internal/session"
)

func TestTerminalTitle(t *testing.T) {
	long := "This is a very long session title that should be clipped"
	tests := []struct {
		name string
		sess *session.Session
		want string
	}{
		{"nil", nil, ""},
		{"empty", &session.Session{}, ""},
		{"blank", &session.Session{Name: "  "}, ""},
		{"short", &session.Session{Name: "Fix flaky tests"}, "Fix flaky tests"},
		{"long", &session.Session{Name: long}, truncateRight(long, 40)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalTitle(tt.sess); got != tt.want {
				t.Errorf("terminalTitle() = %q, want %q", got, tt.want)
			}
		})
	}
	if got := terminalTitle(&session.Session{Name: long}); got == long || len(got) >= len(long) {
		t.Fatalf("expected truncation, got %q", got)
	}
}
