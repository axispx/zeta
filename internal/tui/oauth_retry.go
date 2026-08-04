package tui

import (
	"context"
	"errors"
	"strings"

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
	if m.turn == nil {
		return nil
	}
	if errors.Is(err, ai.ErrAuth) && !m.authRetried && !m.turn.progressed && m.canRetryOAuth() {
		m.authRetried = true
		return m.startAuthRetry()
	}
	return m.surfaceTurnErr(err)
}

// canRetryOAuth reports whether a 401 can be recovered via the active
// provider's refresh token. Dead sessions and API-key providers are skipped.
func (m *Model) canRetryOAuth() bool {
	choice, ok := m.cfg.ActiveChoice()
	if !ok {
		return false
	}
	p, ok := m.cfg.Provider(choice.ProviderID)
	if !ok || p.OAuth == nil || p.OAuth.RefreshFailed {
		return false
	}
	return strings.TrimSpace(p.OAuth.RefreshToken) != ""
}

// startAuthRetry finishes the failed turn and recovers OAuth off the UI thread.
// History is intact (no progress), so a successful result restarts the turn.
func (m *Model) startAuthRetry() tea.Cmd {
	choice, ok := m.cfg.ActiveChoice()
	if !ok {
		return m.surfaceTurnErr(ai.ErrAuth)
	}
	m.finishTurn()
	m.authRetrying = true
	m.layoutPreservingBottom() // busy gap stays up while recover runs

	providerID := choice.ProviderID
	cfg := m.cfg.Clone()
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
	m.authRetrying = false
}

// handleAuthRetryResult installs recovered credentials and restarts the turn,
// or surfaces the error when recovery failed. No-ops the restart when the user
// cancelled the recover wait (authRetrying already cleared).
func (m *Model) handleAuthRetryResult(msg authRetryResultMsg) tea.Cmd {
	if !m.authRetrying {
		// Esc/Ctrl+C abandoned the wait — still install whatever recover wrote
		// (fresh pair or RefreshFailed) so the next submit matches disk, but
		// do not auto-restart the turn.
		if msg.oauth != nil {
			m.installOAuth(msg.providerID, msg.oauth)
		}
		return nil
	}
	m.authRetrying = false
	if msg.oauth != nil {
		m.installOAuth(msg.providerID, msg.oauth)
	}
	if msg.err != nil {
		return m.surfaceTurnErr(msg.err)
	}
	if m.client == nil {
		return m.surfaceTurnErr(ai.ErrAuth)
	}
	// Match submit: workspace snapshot at the turn boundary.
	m.refreshWorkspace()
	return m.beginTurn(firstUserPrompt(m.messages))
}

// installOAuth writes oc onto the live config and rebuilds the API client.
func (m *Model) installOAuth(providerID string, oc *config.OAuthCredential) {
	if oc == nil {
		return
	}
	p, ok := m.cfg.Providers[providerID]
	if !ok {
		return
	}
	cpy := *oc
	p.OAuth = &cpy
	p.APIKey = ""
	if m.cfg.Providers == nil {
		m.cfg.Providers = map[string]config.Provider{}
	}
	m.cfg.Providers[providerID] = p
	m.applyClient()
}

// surfaceTurnErr appends a durable error row for a failed turn. The turn must
// already be finished (or nil).
func (m *Model) surfaceTurnErr(err error) tea.Cmd {
	if m.turn != nil {
		m.finishTurn()
	}
	if err == nil {
		return nil
	}
	errMsg := Message{Role: RoleError, Text: err.Error()}
	m.messages = append(m.messages, errMsg)
	m.persist(session.Record{Role: session.RoleError, Text: errMsg.Text})
	m.refreshTranscript()
	return nil
}
