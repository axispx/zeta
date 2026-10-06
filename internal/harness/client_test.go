package harness

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axispx/zeta/internal/codex"
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

// codexCfg is a Codex-backed provider with OAuth credentials.
func codexCfg(baseURL string) config.Config {
	return config.Config{
		Active: "openai/gpt-5.5",
		Providers: map[string]config.Provider{
			"openai": {
				Name:    "OpenAI",
				BaseURL: baseURL,
				OAuth: &config.OAuthCredential{
					AccessToken:  "at",
					RefreshToken: "rt",
					AccountID:    "acct-1",
				},
				Models: map[string]config.ModelDef{"gpt-5.5": {ContextWindow: 272000}},
			},
		},
	}
}

func TestBillingOf(t *testing.T) {
	t.Parallel()
	// A subscription account is metered by plan; an API key is billed per token.
	if b := BillingOf(codexCfg("https://chatgpt.com/backend-api/codex")); !b.Plan || b.Provider != "OpenAI" {
		t.Fatalf("oauth billing = %#v", b)
	}
	key := clientCfg(config.Provider{BaseURL: "http://127.0.0.1:1", APIKey: "k"})
	if b := BillingOf(key); b.Plan || b.Provider != "x" {
		t.Fatalf("api-key billing = %#v", b)
	}
	if b := BillingOf(config.Config{}); b.Plan || b.Provider != "" {
		t.Fatalf("no active choice = %#v", b)
	}
}

func TestSubscriptionPlan(t *testing.T) {
	t.Parallel()
	// Only the Codex backend meters by subscription quota.
	if _, ok := SubscriptionPlan(codexCfg("https://chatgpt.com/backend-api/codex")); !ok {
		t.Fatal("codex backend must report a plan")
	}
	// An API-key provider on the same host has no quota to read.
	if _, ok := SubscriptionPlan(clientCfg(config.Provider{
		BaseURL: "https://chatgpt.com/backend-api/codex",
		APIKey:  "k",
	})); ok {
		t.Fatal("an api-key provider must not report a plan")
	}
	// Neither does an OpenAI-compatible endpoint behind OAuth.
	if _, ok := SubscriptionPlan(clientCfg(config.Provider{
		BaseURL: "https://api.x.ai/v1",
		OAuth:   &config.OAuthCredential{AccessToken: "at"},
	})); ok {
		t.Fatal("a per-token provider must not report a plan")
	}
	// OAuth without a usable token cannot read one.
	if _, ok := SubscriptionPlan(codexCfg("https://chatgpt.com/backend-api/codex")); !ok {
		t.Fatal("sanity: codex with a token reports a plan")
	}
	noToken := codexCfg("https://chatgpt.com/backend-api/codex")
	p := noToken.Providers["openai"]
	p.OAuth = &config.OAuthCredential{RefreshToken: "rt"}
	noToken.Providers["openai"] = p
	if _, ok := SubscriptionPlan(noToken); ok {
		t.Fatal("an empty access token must not report a plan")
	}
}

func TestPlanQuota(t *testing.T) {
	var gotPath, gotAuth, gotAccount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotAccount = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("chatgpt-account-id")
		_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":12.5,"limit_window_seconds":18000,"reset_at":1704069000}},"credits":{"has_credits":false,"unlimited":false}}`))
	}))
	t.Cleanup(srv.Close)

	// The provider keeps the real Codex base URL so the endpoint gate passes;
	// the quota URL is redirected, like the OAuth token URLs in their tests.
	prev := codex.UsageURL
	codex.UsageURL = srv.URL + codex.UsagePath
	t.Cleanup(func() { codex.UsageURL = prev })

	plan, err := PlanQuota(context.Background(), codexCfg(codex.BaseURL))
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != codex.UsagePath || gotAuth != "Bearer at" || gotAccount != "acct-1" {
		t.Fatalf("path=%q auth=%q account=%q", gotPath, gotAuth, gotAccount)
	}
	if plan.Primary == nil || plan.Primary.UsedPercent != 12.5 || plan.Primary.WindowMinutes != 300 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Credits == nil || plan.Credits.HasCredits {
		t.Fatalf("credits = %#v", plan.Credits)
	}

	// A per-token provider has no quota to read, so no request is made.
	gotPath = ""
	if plan, err := PlanQuota(context.Background(), clientCfg(config.Provider{
		BaseURL: codex.BaseURL,
		APIKey:  "k",
	})); err != nil || plan != nil || gotPath != "" {
		t.Fatalf("api-key provider: plan=%#v err=%v path=%q", plan, err, gotPath)
	}

	// A backend failure surfaces as an error, never as a panic or a zero plan.
	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"Unauthorized"}`))
	}))
	t.Cleanup(srvErr.Close)
	codex.UsageURL = srvErr.URL
	if _, err := PlanQuota(context.Background(), codexCfg(codex.BaseURL)); err == nil {
		t.Fatal("expected error")
	}
}
