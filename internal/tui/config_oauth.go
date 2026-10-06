package tui

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/oauth"
)

type authMethodKind int

const (
	authOAuth authMethodKind = iota
	authAPIKey
)

type authMethodRow struct {
	kind authMethodKind
	name string
	hint string
}

// authMethodRows is the chooser for a provider with an OAuth login. A browser
// flow and a device flow differ only in what the panel shows while waiting, so
// they share the row.
func authMethodRows(providerID string) []authMethodRow {
	hint := "sign in with your account"
	if providerID == codex.ProviderID {
		hint = "sign in with ChatGPT"
	}
	return []authMethodRow{
		{authOAuth, "OAuth", hint},
		{authAPIKey, "API Key", "paste key"},
	}
}

// oauthSession is in-flight OAuth UI state. Completion is a tea.Cmd → Msg;
// no shared run struct or tick poller.
type oauthSession struct {
	gen       int
	cancel    context.CancelFunc
	flow      oauth.Flow
	verifyURL string
	userCode  string
}

type oauthStartedMsg struct {
	gen  int
	ctx  context.Context // the login's lifetime; cancelled with the session
	flow oauth.Flow
	err  error
}

type oauthDoneMsg struct {
	gen int
	tok *oauth.TokenResponse
	// preset is the catalog the provider's backend reported, for providers
	// whose models are discovered rather than listed by models.dev (codex).
	preset *config.Preset
	err    error
}

func (d *configDialog) openAuthMethods(providerID string) {
	if !d.caps(providerID).authChooser() {
		d.openAPIKeyForm(providerID)
		return
	}
	pre, ok := d.findPreset(providerID)
	if !ok {
		d.status = "unknown provider"
		return
	}
	d.cancelOAuth()
	d.focusID = providerID
	d.status = ""
	d.view = configAuth
	d.form = configForm{}
	d.listSel.clear()
	d.authTitle = "Connect · " + pre.Name
	if _, configured := d.draft.Provider(providerID); configured {
		d.authTitle = "Credentials · " + pre.Name
	}
}

func (d *configDialog) cancelOAuth() {
	if d.oauth == nil {
		return
	}
	if d.oauth.cancel != nil {
		d.oauth.cancel()
	}
	// Releases the callback listener or stops device polling.
	if d.oauth.flow != nil {
		d.oauth.flow.Close()
	}
	d.oauth = nil
}

func (d *configDialog) handleAuthKey(msg tea.KeyPressMsg) tea.Cmd {
	if d.oauth != nil {
		if msg.String() == "esc" {
			d.cancelOAuth()
			d.status = "OAuth cancelled"
		}
		return nil
	}
	key := msg.String()
	rows := authMethodRows(d.focusID)
	n := len(rows)
	if d.move(n, key) {
		d.status = ""
		return nil
	}
	switch key {
	case "enter":
		d.status = ""
		if n == 0 || d.selected >= n {
			return nil
		}
		return d.activateAuthMethod(rows[d.selected])
	case "esc":
		d.status = ""
		if _, ok := d.draft.Provider(d.focusID); ok {
			d.enterModels()
			return nil
		}
		d.view = configPresets
		d.listSel.clear()
		return nil
	}
	return nil
}

func (d *configDialog) activateAuthMethod(row authMethodRow) tea.Cmd {
	switch row.kind {
	case authAPIKey:
		d.openAPIKeyForm(d.focusID)
		return nil
	case authOAuth:
		return d.startOAuth()
	}
	return nil
}

func (d *configDialog) beginOAuth() (gen int, ctx context.Context) {
	d.cancelOAuth()
	d.oauthGen++
	gen = d.oauthGen
	ctx, cancel := context.WithCancel(context.Background())
	d.oauth = &oauthSession{gen: gen, cancel: cancel}
	return gen, ctx
}

// startOAuth begins the provider's interactive login. A device flow hands the
// user a code to type; a browser flow opens the page while the callback is
// being awaited, and a browser that will not open just leaves the link to
// click.
func (d *configDialog) startOAuth() tea.Cmd {
	gen, ctx := d.beginOAuth()
	d.status = "Starting sign-in… (esc to cancel)"
	providerID := d.focusID

	return func() tea.Msg {
		flow, err := oauth.Begin(ctx, providerID)
		return oauthStartedMsg{gen: gen, ctx: ctx, flow: flow, err: err}
	}
}

func (d *configDialog) handleOAuthStarted(msg oauthStartedMsg) tea.Cmd {
	if d.oauth == nil || msg.gen != d.oauth.gen {
		// The dialog moved on (or reopened) while the cmd was in flight.
		if msg.flow != nil {
			msg.flow.Close()
		}
		return nil
	}
	if msg.err != nil {
		d.finishOAuth(nil, nil, msg.err)
		return nil
	}
	d.oauth.flow = msg.flow
	d.oauth.verifyURL = msg.flow.URL()
	d.oauth.userCode = msg.flow.UserCode()
	d.status = ""
	_ = openBrowser(d.oauth.verifyURL)

	// Everything the login needs after the browser — the wait, and the
	// provider's model discovery — runs in the cmd, never in Update.
	ctx := msg.ctx
	flow := msg.flow
	gen := msg.gen
	providerID := d.focusID
	return func() tea.Msg {
		tok, err := flow.Wait(ctx)
		if err != nil {
			return oauthDoneMsg{gen: gen, err: err}
		}
		oc := config.OAuthFromToken(tok)
		if oc == nil {
			return oauthDoneMsg{gen: gen, err: errors.New("token response is not renewable")}
		}
		preset, err := oauthPreset(ctx, providerID, oc)
		if err != nil {
			return oauthDoneMsg{gen: gen, err: err}
		}
		return oauthDoneMsg{gen: gen, tok: tok, preset: preset}
	}
}

func (d *configDialog) handleOAuthDone(msg oauthDoneMsg) tea.Cmd {
	if d.oauth == nil || msg.gen != d.oauth.gen {
		return nil
	}
	d.finishOAuth(msg.tok, msg.preset, msg.err)
	return nil
}

// finishOAuth closes the flow session and applies the outcome: a connect, a
// cancellation, or a failure.
func (d *configDialog) finishOAuth(tok *oauth.TokenResponse, preset *config.Preset, err error) {
	if d.oauth != nil && d.oauth.flow != nil {
		d.oauth.flow.Close()
	}
	d.oauth = nil
	if err != nil {
		if errors.Is(err, context.Canceled) {
			d.status = "OAuth cancelled"
			return
		}
		d.status = "OAuth failed: " + err.Error()
		return
	}
	d.applyOAuthResult(tok, preset)
}

func (d *configDialog) applyOAuthResult(tok *oauth.TokenResponse, preset *config.Preset) {
	oc := config.OAuthFromToken(tok)
	if oc == nil {
		d.status = "OAuth failed: empty token"
		return
	}
	pre, ok := d.connectPreset(oc, preset)
	if !ok {
		d.status = "unknown provider"
		return
	}
	if err := d.mutate(func(c *config.Config) error {
		return c.ConnectPresetOAuth(pre, oc)
	}); err != nil {
		d.status = err.Error()
		return
	}
	d.status = "connected · " + d.focusID + " (OAuth)"
	d.listSel.clear()
	d.enterModels()
}

// connectPreset resolves the preset to connect with: what the login discovered,
// else the provider's models.dev entry.
func (d *configDialog) connectPreset(_ *config.OAuthCredential, discovered *config.Preset) (config.Preset, bool) {
	if discovered != nil {
		return *discovered, true
	}
	return d.findPreset(d.focusID)
}

// oauthPreset runs the provider's post-login catalog discovery. Codex gates its
// model list on the account, so the list has to be read with the token that was
// just issued; every other provider comes from models.dev and needs nothing.
//
// A discovery failure fails the login: a provider whose models all error on
// selection is worse than a retry, and the token would have to be re-minted
// anyway to try again.
func oauthPreset(ctx context.Context, providerID string, oc *config.OAuthCredential) (*config.Preset, error) {
	if providerID != codex.ProviderID {
		return nil, nil
	}
	models, err := codex.Models(ctx, oc.AccessToken, oc.AccountID)
	if err != nil {
		return nil, err
	}
	// Cached so a later /config visit can show the list without a token.
	_ = codex.SaveModels(models)
	pre := config.CodexPreset(models)
	return &pre, nil
}

func openBrowser(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}
