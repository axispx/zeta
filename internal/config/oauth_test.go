package config

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
	// Access-only is not renewable under xAI rotation.
	if OAuthFromToken(&oauth.TokenResponse{AccessToken: "a"}) != nil {
		t.Fatal("missing refresh_token must be rejected")
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

	// No expires_in → default TTL so proactive refresh still works.
	oc = OAuthFromToken(&oauth.TokenResponse{AccessToken: "a", RefreshToken: "r"})
	if oc == nil || oc.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("default TTL: got %#v", oc)
	}
}

func TestApplyToken(t *testing.T) {
	t.Parallel()
	oc := &OAuthCredential{
		AccessToken:   "old",
		RefreshToken:  "r",
		ExpiresAt:     1,
		TokenType:     "bearer",
		RefreshFailed: true,
	}
	oc.ApplyToken(&oauth.TokenResponse{
		AccessToken: "new",
		ExpiresIn:   3600,
	})
	if oc.AccessToken != "new" || oc.RefreshToken != "r" {
		t.Fatalf("got %#v", oc)
	}
	if oc.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("expires_at %d", oc.ExpiresAt)
	}
	if oc.RefreshFailed {
		t.Fatal("successful refresh must clear RefreshFailed")
	}

	// A rotated refresh token replaces the old one.
	oc.ApplyToken(&oauth.TokenResponse{
		AccessToken:  "new2",
		RefreshToken: "r2",
	})
	if oc.RefreshToken != "r2" {
		t.Fatalf("rotated refresh token: %q", oc.RefreshToken)
	}
}

func TestOAuthNeedsRefresh(t *testing.T) {
	t.Parallel()
	if oauthNeedsRefresh(nil) {
		t.Fatal("nil")
	}
	// Fresh token.
	if oauthNeedsRefresh(&OAuthCredential{
		RefreshToken: "r",
		ExpiresAt:    time.Now().UnixMilli() + 60*60*1000,
	}) {
		t.Fatal("fresh")
	}
	// Unknown expiry must not burn the RT proactively.
	if oauthNeedsRefresh(&OAuthCredential{RefreshToken: "r", ExpiresAt: 0}) {
		t.Fatal("unknown expiry")
	}
	// Near expiry.
	if !oauthNeedsRefresh(&OAuthCredential{
		RefreshToken: "r",
		ExpiresAt:    time.Now().UnixMilli() - 1,
	}) {
		t.Fatal("expired")
	}
	// Dead short-circuit.
	if oauthNeedsRefresh(&OAuthCredential{
		RefreshToken:  "r",
		ExpiresAt:     time.Now().UnixMilli() - 1,
		RefreshFailed: true,
	}) {
		t.Fatal("dead")
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

func TestRedeemRefreshRequiresRotatedRT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Access-only response — xAI always rotates; treat as failure at commit.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"expires_in":   3600,
			"token_type":   "bearer",
		})
	}))
	t.Cleanup(srv.Close)

	prev := oauth.XaiTokenURL
	oauth.XaiTokenURL = srv.URL
	t.Cleanup(func() { oauth.XaiTokenURL = prev })

	t.Setenv("ZETA_HOME", t.TempDir())
	c := Config{Providers: map[string]Provider{
		"xai": {
			BaseURL: "https://api.x.ai/v1",
			OAuth:   &OAuthCredential{AccessToken: "old", RefreshToken: "r", ExpiresAt: time.Now().UnixMilli() - 1},
			Models:  map[string]ModelDef{},
		},
	}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	ok, err := c.EnsureOAuthFresh(context.Background(), "xai")
	if err == nil || ok {
		t.Fatalf("expected error for missing refresh_token, got refreshed=%v err=%v", ok, err)
	}
	if got := c.Providers["xai"].OAuth.AccessToken; got != "old" {
		t.Fatalf("must not mutate on bad response: %q", got)
	}
}

func TestRecoverOAuthRefreshes(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
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

	c := Config{Providers: map[string]Provider{
		"xai": {
			BaseURL: "https://api.x.ai/v1",
			// Fresh expiry — Recover ignores the skew gate.
			OAuth:  &OAuthCredential{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().UnixMilli() + 60*60*1000},
			Models: map[string]ModelDef{},
		},
	}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	refreshed, err := c.RecoverOAuth(context.Background(), "xai")
	if err != nil || !refreshed {
		t.Fatalf("refreshed=%v err=%v", refreshed, err)
	}
	if got := c.Providers["xai"].OAuth.AccessToken; got != "new-access" {
		t.Fatalf("access token = %q", got)
	}
	if got := c.Providers["xai"].OAuth.RefreshToken; got != "new-refresh" {
		t.Fatalf("refresh token = %q", got)
	}
	disk, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if disk.Providers["xai"].OAuth.AccessToken != "new-access" {
		t.Fatalf("disk access token = %q", disk.Providers["xai"].OAuth.AccessToken)
	}
	if disk.Providers["xai"].OAuth.RefreshToken != "new-refresh" {
		t.Fatalf("disk refresh token = %q", disk.Providers["xai"].OAuth.RefreshToken)
	}
}

func TestEnsureOAuthFreshSkipsFreshToken(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(srv.Close)
	prev := oauth.XaiTokenURL
	oauth.XaiTokenURL = srv.URL
	t.Cleanup(func() { oauth.XaiTokenURL = prev })

	c := Config{Providers: map[string]Provider{
		"xai": {
			BaseURL: "https://api.x.ai/v1",
			OAuth:   &OAuthCredential{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().UnixMilli() + 60*60*1000},
			Models:  map[string]ModelDef{},
		},
	}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	refreshed, err := c.EnsureOAuthFresh(context.Background(), "xai")
	if err != nil || refreshed {
		t.Fatalf("refreshed=%v err=%v", refreshed, err)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestEnsureOAuthFreshRejectedPersistsDead(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	t.Cleanup(srv.Close)

	prev := oauth.XaiTokenURL
	oauth.XaiTokenURL = srv.URL
	t.Cleanup(func() { oauth.XaiTokenURL = prev })

	c := Config{Providers: map[string]Provider{
		"xai": {
			BaseURL: "https://api.x.ai/v1",
			OAuth:   &OAuthCredential{AccessToken: "stale", RefreshToken: "stale-refresh", ExpiresAt: time.Now().UnixMilli() - 1},
			Models:  map[string]ModelDef{},
		},
	}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	refreshed, err := c.EnsureOAuthFresh(context.Background(), "xai")
	if !errors.Is(err, ErrReauthRequired) || !refreshed {
		// refreshed=true: in-memory flag mutated + persisted
		t.Fatalf("refreshed=%v err=%v", refreshed, err)
	}
	if !c.Providers["xai"].OAuth.RefreshFailed {
		t.Fatal("dead flag not kept in memory")
	}
	disk, derr := Load()
	if derr != nil {
		t.Fatal(derr)
	}
	if !disk.Providers["xai"].OAuth.RefreshFailed {
		t.Fatal("dead flag not persisted to disk")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}

	// Later calls short-circuit — the doomed refresh is not replayed.
	refreshed, err = c.RecoverOAuth(context.Background(), "xai")
	if !errors.Is(err, ErrReauthRequired) || refreshed {
		t.Fatalf("short-circuit: refreshed=%v err=%v", refreshed, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits after short-circuit = %d", hits.Load())
	}
}

func TestEnsureOAuthFreshAdoptsDiskBeforeRefresh(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)

	// Another zeta instance already refreshed and saved a fresh pair.
	disk := Config{Providers: map[string]Provider{
		"xai": {
			BaseURL: "https://api.x.ai/v1",
			OAuth:   &OAuthCredential{AccessToken: "disk-access", RefreshToken: "disk-refresh", ExpiresAt: time.Now().UnixMilli() + 3600*1000},
			Models:  map[string]ModelDef{},
		},
	}}
	if err := disk.Save(); err != nil {
		t.Fatal(err)
	}

	// Stale memory would get invalid_grant if it hit the network with its RT.
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	t.Cleanup(srv.Close)
	prev := oauth.XaiTokenURL
	oauth.XaiTokenURL = srv.URL
	t.Cleanup(func() { oauth.XaiTokenURL = prev })

	c := Config{Providers: map[string]Provider{
		"xai": {
			BaseURL: "https://api.x.ai/v1",
			OAuth:   &OAuthCredential{AccessToken: "stale-access", RefreshToken: "stale-refresh", ExpiresAt: time.Now().UnixMilli() - 1},
			Models:  map[string]ModelDef{},
		},
	}}
	refreshed, err := c.EnsureOAuthFresh(context.Background(), "xai")
	if err != nil {
		t.Fatalf("expected adopt, got %v", err)
	}
	if !refreshed {
		t.Fatal("expected credential change")
	}
	if got := c.Providers["xai"].OAuth.AccessToken; got != "disk-access" {
		t.Fatalf("access token = %q", got)
	}
	if got := c.Providers["xai"].OAuth.RefreshToken; got != "disk-refresh" {
		t.Fatalf("refresh token = %q", got)
	}
	// Adopted fresh expiry — must not burn the (stale) RT on the network.
	if hits.Load() != 0 {
		t.Fatalf("network hits = %d, want 0 (disk-first)", hits.Load())
	}
}

func TestRecoverOAuthAdoptsDiskWithoutRefresh(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())

	disk := Config{Providers: map[string]Provider{
		"xai": {
			BaseURL: "https://api.x.ai/v1",
			OAuth:   &OAuthCredential{AccessToken: "disk-access", RefreshToken: "disk-refresh", ExpiresAt: time.Now().UnixMilli() + 3600*1000},
			Models:  map[string]ModelDef{},
		},
	}}
	if err := disk.Save(); err != nil {
		t.Fatal(err)
	}

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	t.Cleanup(srv.Close)
	prev := oauth.XaiTokenURL
	oauth.XaiTokenURL = srv.URL
	t.Cleanup(func() { oauth.XaiTokenURL = prev })

	c := Config{Providers: map[string]Provider{
		"xai": {
			BaseURL: "https://api.x.ai/v1",
			OAuth:   &OAuthCredential{AccessToken: "stale-access", RefreshToken: "stale-refresh", ExpiresAt: time.Now().UnixMilli() + 60*60*1000},
			Models:  map[string]ModelDef{},
		},
	}}
	// Recover (401 path): adopt rotated disk pair instead of redeeming stale RT.
	refreshed, err := c.RecoverOAuth(context.Background(), "xai")
	if err != nil {
		t.Fatalf("expected adopt, got %v", err)
	}
	if !refreshed {
		t.Fatal("expected credential change")
	}
	if got := c.Providers["xai"].OAuth.AccessToken; got != "disk-access" {
		t.Fatalf("access token = %q", got)
	}
	if hits.Load() != 0 {
		t.Fatalf("network hits = %d, want 0", hits.Load())
	}
}

func TestEnsureOAuthFreshDoesNotClobberUnrelatedDiskFields(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
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

	// Disk has an extra model and a different Active than memory.
	disk := Config{
		Active: "xai/disk-model",
		Providers: map[string]Provider{
			"xai": {
				BaseURL: "https://api.x.ai/v1",
				Name:    "DiskName",
				OAuth:   &OAuthCredential{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().UnixMilli() - 1},
				Models: map[string]ModelDef{
					"disk-model": {ContextWindow: 1000},
					"mem-model":  {ContextWindow: 2000},
				},
			},
		},
	}
	if err := disk.Save(); err != nil {
		t.Fatal(err)
	}

	// Memory is missing disk-model and has a different Active.
	c := Config{
		Active: "xai/mem-model",
		Providers: map[string]Provider{
			"xai": {
				BaseURL: "https://api.x.ai/v1",
				Name:    "MemName",
				OAuth:   &OAuthCredential{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().UnixMilli() - 1},
				Models:  map[string]ModelDef{"mem-model": {ContextWindow: 2000}},
			},
		},
	}
	if _, err := c.EnsureOAuthFresh(context.Background(), "xai"); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Active != "xai/disk-model" {
		t.Fatalf("Active clobbered: %q", loaded.Active)
	}
	if loaded.Providers["xai"].Name != "DiskName" {
		t.Fatalf("Name clobbered: %q", loaded.Providers["xai"].Name)
	}
	if _, ok := loaded.Providers["xai"].Models["disk-model"]; !ok {
		t.Fatal("disk-model dropped")
	}
	if got := loaded.Providers["xai"].OAuth.AccessToken; got != "new-access" {
		t.Fatalf("oauth not patched: %q", got)
	}
}
