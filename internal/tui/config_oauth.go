package tui

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/oauth"
)

type authMethodKind int

const (
	authDevice authMethodKind = iota
	authAPIKey
)

type authMethodRow struct {
	kind authMethodKind
	name string
	hint string
}

func authMethodRows() []authMethodRow {
	return []authMethodRow{
		{authDevice, "OAuth", "sign in with your account"},
		{authAPIKey, "API Key", "paste key"},
	}
}

// oauthSession is in-flight OAuth UI state. Completion is a tea.Cmd → Msg;
// no shared run struct or tick poller.
type oauthSession struct {
	gen       int
	ctx       context.Context
	cancel    context.CancelFunc
	verifyURL string
	userCode  string
}

type oauthDeviceMsg struct {
	gen    int
	device oauth.DeviceCode
	err    error
}

type oauthDoneMsg struct {
	gen int
	tok *oauth.TokenResponse
	err error
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
	rows := authMethodRows()
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
	case authDevice:
		return d.startDeviceOAuth()
	}
	return nil
}

func (d *configDialog) beginOAuth() (gen int, ctx context.Context) {
	d.cancelOAuth()
	d.oauthGen++
	gen = d.oauthGen
	ctx, cancel := context.WithCancel(context.Background())
	d.oauth = &oauthSession{gen: gen, ctx: ctx, cancel: cancel}
	return gen, ctx
}

func (d *configDialog) startDeviceOAuth() tea.Cmd {
	gen, ctx := d.beginOAuth()
	d.status = "Starting device login… (esc to cancel)"
	providerID := d.focusID

	return func() tea.Msg {
		device, err := oauth.StartDevice(ctx, providerID)
		return oauthDeviceMsg{gen: gen, device: device, err: err}
	}
}

func (d *configDialog) handleOAuthDevice(msg oauthDeviceMsg) tea.Cmd {
	if d.oauth == nil || msg.gen != d.oauth.gen {
		return nil
	}
	if msg.err != nil {
		return d.finishOAuth(nil, msg.err)
	}
	d.oauth.verifyURL = msg.device.BrowserURL()
	d.oauth.userCode = msg.device.UserCode
	d.status = ""
	_ = openBrowser(d.oauth.verifyURL)

	ctx := d.oauth.ctx
	providerID := d.focusID
	device := msg.device
	gen := msg.gen
	return func() tea.Msg {
		tok, err := oauth.PollDevice(ctx, providerID, device)
		return oauthDoneMsg{gen: gen, tok: tok, err: err}
	}
}

func (d *configDialog) handleOAuthDone(msg oauthDoneMsg) tea.Cmd {
	if d.oauth == nil || msg.gen != d.oauth.gen {
		return nil
	}
	return d.finishOAuth(msg.tok, msg.err)
}

func (d *configDialog) finishOAuth(tok *oauth.TokenResponse, err error) tea.Cmd {
	// Flow finished — drop session without cancel (Cmd already returned).
	d.oauth = nil
	if err != nil {
		if errors.Is(err, context.Canceled) {
			d.status = "OAuth cancelled"
			return nil
		}
		d.status = "OAuth failed: " + err.Error()
		return nil
	}
	return d.applyOAuthResult(tok)
}

func (d *configDialog) applyOAuthResult(tok *oauth.TokenResponse) tea.Cmd {
	oc := config.OAuthFromToken(tok)
	if oc == nil {
		d.status = "OAuth failed: empty token"
		return nil
	}
	pre, ok := d.findPreset(d.focusID)
	if !ok {
		d.status = "unknown provider"
		return nil
	}
	if err := d.mutate(func(c *config.Config) error {
		return c.ConnectPresetOAuth(pre, oc)
	}); err != nil {
		d.status = err.Error()
		return nil
	}
	d.status = "connected · " + d.focusID + " (OAuth)"
	d.listSel.clear()
	d.enterModels()
	return nil
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
