package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/session"
)

// authRetryResultMsg is the outcome of an async RecoverOAuth after a 401.
// The cmd works on a config clone so Bubble Tea's value-receiver Update
// cannot lose in-memory credential mutations; the handler installs oauth.
type authRetryResultMsg struct {
	providerID string
	oauth      *config.OAuthCredential // snapshot after recover (may be dead)
	err        error
}

// handleTurnErr surfaces a turn failure, or arms one async OAuth recovery when
// the provider rejected the credential before any progress.
func (m *Model) handleTurnErr(err error) tea.Cmd {
	if m.turn.current == nil {
		return nil
	}
	if errors.Is(err, ai.ErrAuth) && !m.session.AuthRetried && m.session.CanReplay() && m.session.CanRetryOAuth() {
		m.session.AuthRetried = true
		return m.startAuthRetry()
	}
	return m.surfaceTurnErr(err)
}

// startAuthRetry finishes the failed turn and recovers OAuth off the UI thread.
// History is intact (no progress), so a successful result restarts the turn.
func (m *Model) startAuthRetry() tea.Cmd {
	choice, ok := m.session.Cfg.ActiveChoice()
	if !ok {
		return m.surfaceTurnErr(ai.ErrAuth)
	}
	m.finishTurn()
	m.session.AuthRetrying = true
	m.layoutPreservingBottom() // busy gap stays up while recover runs

	providerID := choice.ProviderID
	cfg := m.session.Cfg.Clone()
	return tea.Batch(func() tea.Msg {
		_, err := cfg.RecoverOAuth(context.Background(), providerID)
		msg := authRetryResultMsg{providerID: providerID, err: err}
		if p, ok := cfg.Providers[providerID]; ok && p.OAuth != nil {
			oc := *p.OAuth
			msg.oauth = &oc
		}
		return msg
	}, m.spinner.Tick)
}

// cancelAuthRetry abandons the recover wait (Esc/Ctrl+C/quit). The async
// RecoverOAuth still finishes; handleAuthRetryResult installs credentials
// without restarting the turn when authRetrying is already false.
func (m *Model) cancelAuthRetry() {
	m.session.AuthRetrying = false
}

// handleAuthRetryResult installs recovered credentials and restarts the turn,
// or surfaces the error when recovery failed. No-ops the restart when the user
// cancelled the recover wait (authRetrying already cleared).
func (m *Model) handleAuthRetryResult(msg authRetryResultMsg) tea.Cmd {
	if !m.session.AuthRetrying {
		// Esc/Ctrl+C abandoned the wait — still install whatever recover wrote
		// (fresh pair or RefreshFailed) so the next submit matches disk, but
		// do not auto-restart the turn.
		if msg.oauth != nil {
			m.session.InstallOAuth(msg.providerID, msg.oauth)
		}
		return nil
	}
	m.session.AuthRetrying = false
	if msg.oauth != nil {
		m.session.InstallOAuth(msg.providerID, msg.oauth)
	}
	if msg.err != nil {
		return m.surfaceTurnErr(msg.err)
	}
	if m.session.Client == nil {
		return m.surfaceTurnErr(ai.ErrAuth)
	}
	// Match submit: workspace snapshot at the turn boundary.
	m.session.RefreshWorkspace()
	return m.beginTurn(firstUserPrompt(m.transcript.messages))
}

// surfaceTurnErr appends a durable error row for a failed turn. The turn must
// already be finished (or nil).
func (m *Model) surfaceTurnErr(err error) tea.Cmd {
	if m.turn.current != nil {
		m.finishTurn()
	}
	if err == nil {
		return nil
	}
	errMsg := Message{Role: RoleError, Text: err.Error()}
	m.transcript.messages = append(m.transcript.messages, errMsg)
	m.persist(session.Record{Role: session.RoleError, Text: errMsg.Text})
	m.refreshTranscript()
	return nil
}
