package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/version"
)

// withVersion pins version.Version for the duration of a test.
func withVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

func TestRunCommandUpdateRequestsRestart(t *testing.T) {
	// Both a release build and a dev build quit: main applies the release, or a
	// synthetic update when there is no release to download.
	for _, v := range []string{"dev", "0.10.0"} {
		t.Run(v, func(t *testing.T) {
			withVersion(t, v)
			m := testModel()
			cmd := m.runCommand("/update")
			if !m.exit.updateOnExit {
				t.Fatal("expected update request")
			}
			if !m.exit.quitting {
				t.Fatal("expected the TUI to quit")
			}
			if cmd == nil {
				t.Fatal("expected a quit command")
			}
			if msg := cmd(); msg != tea.Quit() {
				t.Fatalf("cmd() = %T, want tea.QuitMsg", msg)
			}
		})
	}
}

func TestHandleUpdateAvailable(t *testing.T) {
	m := testModel()
	m.handleUpdateAvailable(updateAvailableMsg{from: "0.10.0", to: "0.11.0"})
	want := "zeta 0.11.0 available (you have 0.10.0) — run /update"
	if n := len(m.transcript.messages); n == 0 || m.transcript.messages[n-1].Text != want {
		t.Fatalf("messages=%+v", m.transcript.messages)
	}
}

func TestHandleUpdateAvailableQuietOnQuit(t *testing.T) {
	m := testModel()
	m.exit.quitting = true
	m.handleUpdateAvailable(updateAvailableMsg{from: "0.10.0", to: "0.11.0"})
	if len(m.transcript.messages) != 0 {
		t.Fatalf("messages=%+v", m.transcript.messages)
	}
}

func TestCheckUpdateCmdSkipsDev(t *testing.T) {
	// version.Version is "dev" in tests (no ldflags).
	if cmd := checkUpdateCmd(); cmd != nil {
		t.Fatal("dev build should not schedule a check")
	}
}

func TestExclusiveJob(t *testing.T) {
	m := testModel()
	if m.exclusiveJob() {
		t.Fatal("idle")
	}
	m.session.Compacting = true
	if !m.exclusiveJob() || !m.busy() {
		t.Fatal("compacting")
	}
	m.session.Compacting = false
	m.session.AuthRetrying = true
	if m.exclusiveJob() {
		t.Fatal("auth is busy but not exclusive")
	}
	if !m.busy() {
		t.Fatal("auth should still be busy")
	}
}
