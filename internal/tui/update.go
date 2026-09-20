package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/update"
	"github.com/axispx/zeta/internal/version"
)

const (
	// Startup check is best-effort; keep it short so a slow network does not
	// leave a long-lived goroutine hanging around after quit.
	updateCheckTimeout = 8 * time.Second
)

// updateAvailableMsg is a silent startup version check result.
type updateAvailableMsg struct {
	from, to string
}

// checkUpdateCmd probes GitHub for a newer release (no download, no busy UI).
// Skips dev builds. Failures are silent.
func checkUpdateCmd() tea.Cmd {
	if update.IsDev(version.Version) {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		res, err := update.Check(ctx, update.Options{Current: version.Version})
		if err != nil || res.AlreadyLatest {
			return nil
		}
		return updateAvailableMsg{from: res.From, to: res.To}
	}
}

// handleUpdateAvailable notes a one-line nudge when a newer release exists.
func (m *Model) handleUpdateAvailable(msg updateAvailableMsg) {
	if m.exit.quitting || msg.to == "" {
		return
	}
	m.noteSystem(fmt.Sprintf("zeta %s available (you have %s) — run /update", msg.to, msg.from))
}

// requestUpdate closes zeta so main can apply the release in the CLI and relaunch
// it. The download must not run here: replacing the running binary while
// bubbletea owns the terminal would strand the user on a stale process.
// Dev builds have no release to fetch; they take the same path and main applies
// a synthetic update, which keeps the handoff testable without a release.
func (m *Model) requestUpdate() tea.Cmd {
	m.exit.updateOnExit = true
	return m.requestQuit()
}
