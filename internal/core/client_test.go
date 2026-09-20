package core

import (
	"testing"

	"github.com/axispx/zeta/internal/config"
)

func clientCfg(p config.Provider) config.Config {
	if p.Models == nil {
		p.Models = map[string]config.ModelDef{"grok": {ContextWindow: 128000}}
	}
	return config.Config{
		Active:    "x/grok",
		Providers: map[string]config.Provider{"x": p},
	}
}

func TestApplyClientFollowsActiveChoice(t *testing.T) {
	var s Session
	s.ApplyClient()
	if s.Client != nil {
		t.Fatal("no active choice must leave no client")
	}

	s.Cfg = clientCfg(config.Provider{
		BaseURL: "http://127.0.0.1:1",
		APIKey:  "k",
		Models:  map[string]config.ModelDef{"grok": {ContextWindow: 128000}},
	})
	s.ApplyClient()
	if s.Client == nil {
		t.Fatal("a resolvable choice must build a client")
	}
}

func TestCanRetryOAuth(t *testing.T) {
	cases := []struct {
		name string
		p    config.Provider
		want bool
	}{
		{"api key", config.Provider{APIKey: "k"}, false},
		{"refreshable", config.Provider{OAuth: &config.OAuthCredential{RefreshToken: "rt"}}, true},
		{"no refresh token", config.Provider{OAuth: &config.OAuthCredential{AccessToken: "at"}}, false},
		{"dead", config.Provider{OAuth: &config.OAuthCredential{RefreshToken: "rt", RefreshFailed: true}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Session{Cfg: clientCfg(tc.p)}
			if got := s.CanRetryOAuth(); got != tc.want {
				t.Fatalf("CanRetryOAuth = %v, want %v", got, tc.want)
			}
		})
	}
	var empty Session
	if empty.CanRetryOAuth() {
		t.Fatal("no active choice must not retry")
	}
}

func TestInstallOAuthRebuildsClient(t *testing.T) {
	s := Session{Cfg: clientCfg(config.Provider{
		BaseURL: "http://127.0.0.1:1",
		APIKey:  "old",
		OAuth:   &config.OAuthCredential{AccessToken: "at"},
		Models:  map[string]config.ModelDef{"grok": {ContextWindow: 128000}},
	})}
	s.InstallOAuth("x", &config.OAuthCredential{AccessToken: "new-at", RefreshToken: "new-rt"})
	p := s.Cfg.Providers["x"]
	if p.OAuth == nil || p.OAuth.AccessToken != "new-at" || p.OAuth.RefreshToken != "new-rt" {
		t.Fatalf("oauth = %+v", p.OAuth)
	}
	if p.APIKey != "" {
		t.Fatalf("api key must be cleared: %q", p.APIKey)
	}
	if s.Client == nil {
		t.Fatal("client should be rebuilt")
	}
}
