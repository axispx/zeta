package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/axispx/zeta/internal/oauth"
)

func TestAuthToken(t *testing.T) {
	t.Parallel()
	if (Provider{}).AuthToken() != "" {
		t.Fatal("empty")
	}
	if got := (Provider{APIKey: " k "}).AuthToken(); got != "k" {
		t.Fatalf("api key: %q", got)
	}
	if got := (Provider{
		APIKey: "k",
		OAuth:  &OAuthCredential{AccessToken: " tok "},
	}).AuthToken(); got != "tok" {
		t.Fatalf("oauth wins: %q", got)
	}
}

func TestHasUsableCredential(t *testing.T) {
	t.Parallel()
	if (Provider{}).HasUsableCredential() {
		t.Fatal("empty provider")
	}
	if (Provider{OAuth: &OAuthCredential{}}).HasUsableCredential() {
		t.Fatal("empty oauth")
	}
	if !(Provider{APIKey: "k"}).HasUsableCredential() {
		t.Fatal("api key")
	}
	if !(Provider{OAuth: &OAuthCredential{AccessToken: "t"}}).HasUsableCredential() {
		t.Fatal("oauth access")
	}
}

func TestOAuthFromToken(t *testing.T) {
	t.Parallel()
	if OAuthFromToken(nil) != nil || OAuthFromToken(&oauth.TokenResponse{}) != nil {
		t.Fatal("empty")
	}
	oc := OAuthFromToken(&oauth.TokenResponse{
		AccessToken:  "a",
		RefreshToken: "r",
		ExpiresIn:    60,
		TokenType:    "bearer",
	})
	if oc == nil || oc.AccessToken != "a" || oc.RefreshToken != "r" || oc.TokenType != "bearer" {
		t.Fatalf("got %#v", oc)
	}
	if oc.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("expires_at %d", oc.ExpiresAt)
	}
}

func TestPutProviderRejectsEmptyOAuth(t *testing.T) {
	t.Parallel()
	var c Config
	err := c.PutProvider("xai", Provider{
		BaseURL: "https://api.x.ai/v1",
		OAuth:   &OAuthCredential{},
		Models:  map[string]ModelDef{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUpdateProviderClearsOAuth(t *testing.T) {
	t.Parallel()
	c := Config{Providers: map[string]Provider{
		"xai": {
			BaseURL: "https://api.x.ai/v1",
			OAuth:   &OAuthCredential{AccessToken: "t", RefreshToken: "r"},
			Models:  map[string]ModelDef{},
		},
	}}
	if err := c.UpdateProvider("xai", "", "", "new-key"); err != nil {
		t.Fatal(err)
	}
	p := c.Providers["xai"]
	if p.APIKey != "new-key" || p.OAuth != nil {
		t.Fatalf("got %#v", p)
	}
}

func TestRefreshOAuthIfNeededGates(t *testing.T) {
	t.Parallel()

	p := Provider{OAuth: &OAuthCredential{
		AccessToken:  "a",
		RefreshToken: "r",
		ExpiresAt:    time.Now().UnixMilli() + 60*60*1000,
	}}
	ok, err := RefreshOAuthIfNeeded(context.Background(), "xai", &p)
	if err != nil || ok {
		t.Fatalf("fresh token: refreshed=%v err=%v", ok, err)
	}

	ok, err = RefreshOAuthIfNeeded(context.Background(), "openai", &Provider{
		OAuth: &OAuthCredential{AccessToken: "a", RefreshToken: "r", ExpiresAt: 1},
	})
	if err == nil || ok {
		t.Fatalf("unsupported: refreshed=%v err=%v", ok, err)
	}

	ok, err = RefreshOAuthIfNeeded(context.Background(), "xai", &Provider{})
	if err != nil || ok {
		t.Fatalf("nil oauth: refreshed=%v err=%v", ok, err)
	}
}

func TestRefreshOAuthIfNeededUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
			"token_type":    "bearer",
		})
	}))
	t.Cleanup(srv.Close)

	prev := oauth.XaiTokenURL
	oauth.XaiTokenURL = srv.URL
	t.Cleanup(func() { oauth.XaiTokenURL = prev })

	p := Provider{OAuth: &OAuthCredential{
		AccessToken:  "old",
		RefreshToken: "r",
		ExpiresAt:    time.Now().UnixMilli() - 1,
	}}
	ok, err := RefreshOAuthIfNeeded(context.Background(), "xai", &p)
	if err != nil || !ok {
		t.Fatalf("refreshed=%v err=%v", ok, err)
	}
	if p.OAuth.AccessToken != "new-access" || p.OAuth.RefreshToken != "new-refresh" {
		t.Fatalf("got %#v", p.OAuth)
	}
	if p.OAuth.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("expires_at not updated: %d", p.OAuth.ExpiresAt)
	}
}
