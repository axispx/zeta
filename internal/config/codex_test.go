package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/oauth"
)

// codex tokens are not single-use, so a refresh response that omits
// refresh_token is a success, not a reason to re-authenticate.
func TestEnsureOAuthFreshCodexKeepsRefreshToken(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"expires_in":   3600,
			"token_type":   "bearer",
		})
	}))
	t.Cleanup(srv.Close)

	prev := oauth.CodexTokenURL
	oauth.CodexTokenURL = srv.URL
	t.Cleanup(func() { oauth.CodexTokenURL = prev })

	c := Config{
		Active: "openai/gpt-5.5",
		Providers: map[string]Provider{
			"openai": {
				BaseURL: codex.BaseURL,
				OAuth: &OAuthCredential{
					AccessToken:  "old",
					RefreshToken: "r",
					AccountID:    "acct",
					ExpiresAt:    time.Now().UnixMilli() - 1,
				},
				Models: map[string]ModelDef{"gpt-5.5": {ContextWindow: 272000}},
			},
		},
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	ok, err := c.EnsureOAuthFresh(context.Background(), "openai")
	if err != nil || !ok {
		t.Fatalf("refresh: changed=%v err=%v", ok, err)
	}
	oc := c.Providers["openai"].OAuth
	if oc.AccessToken != "new-access" {
		t.Fatalf("access token = %q", oc.AccessToken)
	}
	if oc.RefreshToken != "r" {
		t.Fatalf("refresh token = %q, want the stored one", oc.RefreshToken)
	}
	if oc.AccountID != "acct" {
		t.Fatalf("account id = %q", oc.AccountID)
	}
	if oc.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("expires_at = %d", oc.ExpiresAt)
	}
	// The rotated access token must reach disk, so another process adopts it.
	disk, err := readConfigFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := disk.Providers["openai"].OAuth.AccessToken; got != "new-access" {
		t.Fatalf("persisted access token = %q", got)
	}
	if got := disk.Providers["openai"].OAuth.AccountID; got != "acct" {
		t.Fatalf("persisted account id = %q", got)
	}
}

func TestCodexPreset(t *testing.T) {
	t.Parallel()
	pre := CodexPreset([]codex.Model{
		{Slug: "gpt-6-sol", Name: "GPT-6 Sol", ContextWindow: 272000, Efforts: []string{"low", "high"}},
		{Slug: "gpt-5.5", ContextWindow: 272000},
		// Dropped: unusable entries and duplicates.
		{Slug: "", ContextWindow: 1},
		{Slug: "no-context"},
		{Slug: "gpt-5.5", ContextWindow: 1},
	})
	if pre.ID != codex.ProviderID || pre.BaseURL != codex.BaseURL || pre.Name == "" {
		t.Fatalf("preset = %#v", pre)
	}
	if len(pre.Models) != 2 || pre.DefaultModel != "gpt-6-sol" {
		t.Fatalf("preset = %#v", pre)
	}
	m := pre.Models["gpt-6-sol"]
	if m.ContextWindow != 272000 || len(m.ReasoningEfforts) != 2 {
		t.Fatalf("model = %#v", m)
	}
	// A discovery result with nothing usable still yields a connectable preset
	// (no models yet); connect itself rejects that.
	if pre := CodexPreset(nil); pre.ID != codex.ProviderID || len(pre.Models) != 0 {
		t.Fatalf("empty preset = %#v", pre)
	}
}
