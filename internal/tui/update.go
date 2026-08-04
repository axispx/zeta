package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/update"
	"github.com/axispx/zeta/internal/version"
)

const (
	statusUpdating      = "Updating"
	updateCancelledText = "Update cancelled"
	// Startup check is best-effort; keep it short so a slow network does not
	// leave a long-lived goroutine hanging around after quit.
	updateCheckTimeout = 8 * time.Second
)

// updateDoneMsg is the result of an async /update run.
type updateDoneMsg struct {
	result update.Result
	err    error
}

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
	if m.quitting || msg.to == "" {
		return
	}
	m.noteSystem(fmt.Sprintf("zeta %s available (you have %s) — run /update", msg.to, msg.from))
}

// startUpdate kicks off a background self-update (GitHub Releases).
func (m *Model) startUpdate() tea.Cmd {
	if m.busy() {
		return nil
	}
	if update.IsDev(version.Version) {
		m.noteSystem("cannot update a dev build; install a release binary")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.updateCancel = cancel
	m.updating = true
	m.layoutPreservingBottom()

	return tea.Batch(func() tea.Msg {
		res, err := update.Apply(ctx, update.Options{Current: version.Version})
		return updateDoneMsg{result: res, err: err}
	}, m.spinner.Tick)
}

// cancelUpdate aborts an in-flight update (Esc/Ctrl+C/quit). Clears busy
// immediately (auth-style); the async cmd still returns updateDoneMsg, which
// handleUpdateDone ignores when updating is already false. Caller notes the
// cancel (tryInterrupt) — that refresh shrinks the busy gap; quit stays silent.
func (m *Model) cancelUpdate() {
	if !m.updating {
		return
	}
	if m.updateCancel != nil {
		m.updateCancel()
		m.updateCancel = nil
	}
	m.updating = false
}

func (m *Model) handleUpdateDone(msg updateDoneMsg) tea.Cmd {
	// Esc/Ctrl+C already cleared busy and noted cancellation.
	if !m.updating {
		return nil
	}
	m.updating = false
	m.updateCancel = nil
	if m.quitting {
		return nil
	}
	switch {
	case msg.err == nil && msg.result.AlreadyLatest:
		m.noteSystem(fmt.Sprintf("already on %s", msg.result.From))
	case msg.err == nil:
		m.noteSystem(fmt.Sprintf("updated %s → %s — restart zeta", msg.result.From, msg.result.To))
	case errors.Is(msg.err, context.Canceled):
		m.noteSystem(updateCancelledText)
	default:
		m.noteError(msg.err.Error())
	}
	return nil
}
