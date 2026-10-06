package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/oauth"
)

// openAIPreset is the models.dev entry for the API-key path.
var openAIPreset = config.Preset{
	ID:      codex.ProviderID,
	Name:    codex.DisplayName,
	BaseURL: "https://api.openai.com/v1",
	Models:  map[string]config.ModelDef{"gpt-4.1": {ContextWindow: 1000000}},
}

// authDialogOpen opens /config with the catalog already loaded, so tests do not
// touch the network for presets.
func authDialogOpen(t *testing.T) *configDialog {
	t.Helper()
	isolateZetaHome(t)
	d := &configDialog{}
	d.Open(config.Config{})
	d.loading = false
	d.presets = []config.Preset{openAIPreset}
	return d
}

// OpenAI is one provider with two ways in, like xAI: the catalog entry carries
// the API-key path, and the chooser offers OAuth beside it.
func TestOpenAIOffersBothAuthMethods(t *testing.T) {
	d := authDialogOpen(t)

	pre, ok := d.findPreset(codex.ProviderID)
	if !ok || pre.BaseURL != openAIPreset.BaseURL {
		t.Fatalf("preset = %#v, ok = %v", pre, ok)
	}
	if !d.caps(codex.ProviderID).authChooser() {
		t.Fatal("openai must offer the auth chooser")
	}
	for _, id := range []string{codex.ProviderID, "xai"} {
		rows := authMethodRows(id)
		if len(rows) != 2 || rows[0].kind != authOAuth || rows[1].kind != authAPIKey {
			t.Fatalf("%s rows = %#v", id, rows)
		}
	}
}

// A ChatGPT-connected provider keeps its discovered models on entry; the
// catalog entry's API models must not replace them.
func TestChatGPTProviderSyncsFromDiscovery(t *testing.T) {
	d := authDialogOpen(t)
	if err := codex.SaveModels([]codex.Model{{Slug: "gpt-5.5", Name: "GPT-5.5", ContextWindow: 272000}}); err != nil {
		t.Fatal(err)
	}
	pre := config.CodexPreset(codex.CachedModels())
	if err := d.draft.ConnectPresetOAuth(pre, &config.OAuthCredential{AccessToken: "at", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	got, ok := d.syncPreset(codex.ProviderID)
	if !ok || got.BaseURL != codex.BaseURL || got.Models["gpt-5.5"].ContextWindow != 272000 {
		t.Fatalf("sync preset = %#v", got)
	}

	// Reconnecting with an API key goes back to the catalog entry.
	if err := d.draft.ConnectPreset(openAIPreset, "sk-x"); err != nil {
		t.Fatal(err)
	}
	p, _ := d.draft.Provider(codex.ProviderID)
	if codex.IsEndpoint(p.BaseURL) || p.OAuth != nil || p.APIKey != "sk-x" {
		t.Fatalf("api-key reconnect left %#v", p)
	}
	if got, _ := d.syncPreset(codex.ProviderID); got.BaseURL != openAIPreset.BaseURL {
		t.Fatalf("sync preset = %#v", got)
	}
}

// The browser flow hands back a URL and no code; the panel keys its wording off
// exactly that.
// Signing in must leave a usable provider behind: discovery runs with the fresh
// token, the model list lands in the config, and one model is enabled.
func TestCodexConnectDiscoversModels(t *testing.T) {
	d := authDialogOpen(t)

	var gotAuth, gotAccount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		_, _ = w.Write([]byte(`{"models":[
			{"slug":"gpt-6-sol","display_name":"GPT-6 Sol","visibility":"list","context_window":272000,
			 "supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}]},
			{"slug":"gpt-6-luna","display_name":"GPT-6 Luna","visibility":"list","context_window":272000}
		]}`))
	}))
	t.Cleanup(srv.Close)
	prev := codex.ModelsURL
	codex.ModelsURL = srv.URL + codex.ModelsPath
	t.Cleanup(func() { codex.ModelsURL = prev })

	d.focusID = codex.ProviderID
	d.openAuthMethods(codex.ProviderID)
	d.beginOAuth()
	completeOAuth(t, d, &oauth.TokenResponse{
		AccessToken:  "at",
		RefreshToken: "rt",
		AccountID:    "acct-1",
		ExpiresIn:    3600,
		TokenType:    "bearer",
	})

	if gotAuth != "Bearer at" || gotAccount != "acct-1" {
		t.Fatalf("discovery headers: auth=%q account=%q", gotAuth, gotAccount)
	}

	p, ok := d.draft.Provider(codex.ProviderID)
	if !ok {
		t.Fatalf("provider not connected: %s", d.status)
	}
	if p.OAuth == nil || p.OAuth.AccessToken != "at" || p.OAuth.AccountID != "acct-1" {
		t.Fatalf("oauth = %#v", p.OAuth)
	}
	if len(p.Models) != 2 {
		t.Fatalf("models = %#v", p.Models)
	}
	sol := p.Models["gpt-6-sol"]
	if sol.ContextWindow != 272000 || sol.DisplayName("gpt-6-sol") != "GPT-6 Sol" {
		t.Fatalf("model = %#v", sol)
	}
	if strings.Join(sol.EffortChoices(), ",") != "low,high" {
		t.Fatalf("efforts = %#v", sol.EffortChoices())
	}
	// Like any catalog provider, discovered models arrive Disabled: the user
	// turns on the ones they want (Ctrl+A enables all).
	if sol.Enabled() {
		t.Fatalf("a fresh connect must leave models disabled: %#v", sol)
	}
	if d.draft.Active != "" {
		t.Fatalf("active = %q, want none until a model is enabled", d.draft.Active)
	}
	if err := d.draft.SetModelEnabled(codex.ProviderID, "gpt-6-sol", true); err != nil {
		t.Fatal(err)
	}
	if d.draft.Active != codex.ProviderID+"/gpt-6-sol" {
		t.Fatalf("active = %q", d.draft.Active)
	}
	if err := d.draft.Validate(); err != nil {
		t.Fatalf("saved config must validate: %v", err)
	}
	// The list is cached, so /config can show models before the next sign-in.
	if got := codex.CachedModels(); len(got) != 2 || got[0].Slug != "gpt-6-sol" {
		t.Fatalf("cache = %#v", got)
	}
}

// A discovery failure must fail the connect rather than install a provider
// whose models all error on selection.
func TestCodexConnectDiscoveryFailure(t *testing.T) {
	d := authDialogOpen(t)
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"Unauthorized"}`))
	}))
	t.Cleanup(failing.Close)
	prev := codex.ModelsURL
	codex.ModelsURL = failing.URL
	t.Cleanup(func() { codex.ModelsURL = prev })

	d.focusID = codex.ProviderID
	d.openAuthMethods(codex.ProviderID)
	d.beginOAuth()
	completeOAuth(t, d, &oauth.TokenResponse{AccessToken: "at", RefreshToken: "rt", AccountID: "acct"})

	if _, ok := d.draft.Provider(codex.ProviderID); ok {
		t.Fatal("a failed discovery must not connect the provider")
	}
	if !strings.Contains(d.status, "OAuth failed") {
		t.Fatalf("status = %q", d.status)
	}
}

// A catalog provider still connects from its models.dev preset.
func TestOAuthConnectCatalogProvider(t *testing.T) {
	d := authDialogOpen(t)
	d.presets = []config.Preset{{
		ID:      "xai",
		Name:    "xAI",
		BaseURL: "https://api.x.ai/v1",
		Models:  map[string]config.ModelDef{"grok": {ContextWindow: 128000}},
	}}
	d.focusID = "xai"
	d.openAuthMethods("xai")
	d.beginOAuth()
	completeOAuth(t, d, &oauth.TokenResponse{AccessToken: "at", RefreshToken: "rt", ExpiresIn: 60})

	p, ok := d.draft.Provider("xai")
	if !ok {
		t.Fatalf("provider not connected: %s", d.status)
	}
	if p.OAuth == nil || p.OAuth.RefreshToken != "rt" {
		t.Fatalf("oauth = %#v", p.OAuth)
	}
	if _, ok := p.Models["grok"]; !ok {
		t.Fatalf("catalog models must be kept: %#v", p.Models)
	}
}

// A flow that finishes after the dialog moved on must be closed, not leaked.
func TestStaleOAuthStartedIsClosed(t *testing.T) {
	d := authDialogOpen(t)
	d.focusID = codex.ProviderID
	d.openAuthMethods(codex.ProviderID)
	d.beginOAuth()

	flow, err := oauth.Begin(context.Background(), codex.ProviderID)
	if err != nil {
		t.Fatal(err)
	}
	// A newer generation (or a cleared session) means this result is stale.
	d.cancelOAuth()
	if cmd := d.handleOAuthStarted(oauthStartedMsg{gen: 0, flow: flow}); cmd != nil {
		t.Fatal("a stale start must not be awaited")
	}
	// The flow is already closed, so waiting on it reports cancellation rather
	// than blocking.
	if _, err := flow.Wait(context.Background()); err == nil {
		t.Fatal("expected the stale flow to be unusable")
	}
}

// completeOAuth finishes a login the way beginOAuth's command does: discovery
// runs with the fresh token, then the result lands in the dialog.
func completeOAuth(t *testing.T, d *configDialog, tok *oauth.TokenResponse) {
	t.Helper()
	oc := config.OAuthFromToken(tok)
	if oc == nil {
		t.Fatal("token is not renewable")
	}
	preset, err := oauthPreset(context.Background(), d.focusID, oc)
	d.finishOAuth(tok, preset, err)
}
