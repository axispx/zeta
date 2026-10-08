package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsEndpoint(t *testing.T) {
	t.Parallel()
	yes := []string{
		BaseURL,
		BaseURL + "/",
		"https://chatgpt.com/backend-api/codex",
	}
	for _, u := range yes {
		if !IsEndpoint(u) {
			t.Fatalf("IsEndpoint(%q) = false", u)
		}
	}
	no := []string{
		"",
		"https://api.openai.com/v1",
		"https://chatgpt.com/backend-api",
		"https://chatgpt.com/v1",
		"https://example.com/backend-api/codex",
		"not a url",
	}
	for _, u := range no {
		if IsEndpoint(u) {
			t.Fatalf("IsEndpoint(%q) = true", u)
		}
	}
}

func TestModelsParse(t *testing.T) {
	t.Parallel()
	raw := modelsResponse{}
	if err := json.Unmarshal([]byte(`{"models":[
		{"slug":"gpt-5.3-codex","display_name":"GPT-5.3 Codex","visibility":"list","context_window":272000,
		 "supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}],
		 "service_tiers":[{"id":"flex","name":"Flex"},{"id":"priority","name":"Fast"}]},
		{"slug":"gpt-5.5","display_name":"","visibility":"list","max_context_window":400000},
		{"slug":"gpt-secret","display_name":"Secret","visibility":"hidden","context_window":1000},
		{"slug":"gpt-5.3-codex","display_name":"dup","visibility":"list","context_window":1},
		{"slug":"","display_name":"nameless","visibility":"list","context_window":1}
	]}`), &raw); err != nil {
		t.Fatal(err)
	}
	got := raw.models()
	if len(got) != 2 {
		t.Fatalf("models = %#v", got)
	}
	if got[0].Slug != "gpt-5.3-codex" || got[0].Name != "GPT-5.3 Codex" || got[0].ContextWindow != 272000 {
		t.Fatalf("first = %#v", got[0])
	}
	if strings.Join(got[0].Efforts, ",") != "low,high" {
		t.Fatalf("efforts = %#v", got[0].Efforts)
	}
	if got[0].FastTier != "priority" || got[1].FastTier != "" {
		t.Fatalf("fast tiers = %q, %q", got[0].FastTier, got[1].FastTier)
	}
	// Empty display name falls back to the slug; missing window to max/default.
	if got[1].Slug != "gpt-5.5" || got[1].Name != "gpt-5.5" || got[1].ContextWindow != 400000 {
		t.Fatalf("second = %#v", got[1])
	}
}

func TestModelsRequest(t *testing.T) {
	var gotPath, gotVersion, gotAuth, gotAccount, gotOriginator, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotVersion = r.URL.Query().Get("client_version")
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		gotOriginator = r.Header.Get("originator")
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"models":[{"slug":"m","display_name":"M","visibility":"list","context_window":1}]}`))
	}))
	t.Cleanup(srv.Close)
	redirectModels(t, srv.URL)

	models, err := Models(context.Background(), "token-1", "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Slug != "m" {
		t.Fatalf("models = %#v", models)
	}
	if gotPath != ModelsPath {
		t.Fatalf("path = %q", gotPath)
	}
	// The endpoint answers 400 without a client_version.
	if gotVersion != clientVersion {
		t.Fatalf("client_version = %q", gotVersion)
	}
	if gotAuth != "Bearer token-1" || gotAccount != "acct-1" || gotOriginator == "" || gotUA == "" {
		t.Fatalf("headers: auth=%q account=%q originator=%q ua=%q", gotAuth, gotAccount, gotOriginator, gotUA)
	}
}

// redirectModels points discovery at a test server.
func redirectModels(t *testing.T, base string) {
	t.Helper()
	prev := ModelsURL
	ModelsURL = strings.TrimRight(base, "/") + ModelsPath
	t.Cleanup(func() { ModelsURL = prev })
}

func TestModelsErrors(t *testing.T) {
	// Not parallel: the discovery URL is package state.
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"no"}`))
	}))
	t.Cleanup(forbidden.Close)

	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{`))
	}))
	t.Cleanup(garbage.Close)

	cases := []struct {
		name string
		url  string
	}{
		{"status", forbidden.URL},
		{"parse", garbage.URL},
		{"transport", "http://127.0.0.1:1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			redirectModels(t, tc.url)
			if _, err := Models(context.Background(), "t", ""); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCacheRoundTrip(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	if got := CachedModels(); got != nil {
		t.Fatalf("empty cache = %#v", got)
	}
	want := []Model{{Slug: "gpt-5.5", Name: "GPT-5.5", ContextWindow: 272000, Efforts: []string{"low"}}}
	if err := SaveModels(want); err != nil {
		t.Fatal(err)
	}
	got := CachedModels()
	if len(got) != 1 || got[0].Slug != "gpt-5.5" || got[0].ContextWindow != 272000 {
		t.Fatalf("cache = %#v", got)
	}
	// An empty result must not overwrite a good cache.
	if err := SaveModels(nil); err != nil {
		t.Fatal(err)
	}
	if len(CachedModels()) != 1 {
		t.Fatal("empty save wiped the cache")
	}
}

func TestCacheCorruptFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	if err := os.MkdirAll(filepath.Dir(CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CachePath(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CachedModels(); got != nil {
		t.Fatalf("corrupt cache = %#v", got)
	}
}

func TestPlanFromHeaders(t *testing.T) {
	t.Parallel()
	// No rate-limit headers at all is not an error: the quota is simply
	// unknown, and the caller may read it from the usage endpoint instead.
	h := http.Header{}
	h.Set("content-type", "text/event-stream")
	if p := PlanFromHeaders(h); p != nil {
		t.Fatalf("no headers must yield nil, got %#v", p)
	}
	// Secondary alone is a complete quota.
	only := http.Header{}
	only.Set("x-codex-secondary-used-percent", "80")
	if p := PlanFromHeaders(only); p.Empty() || p.Secondary == nil || p.Primary != nil {
		t.Fatalf("secondary only = %#v", p)
	}
	// A zero percent is real data, not an absent window.
	zero := http.Header{}
	zero.Set("x-codex-primary-used-percent", "0")
	p := PlanFromHeaders(zero)
	if p.Primary == nil || p.Primary.UsedPercent != 0 {
		t.Fatalf("zero percent = %#v", p)
	}
	// Garbage in a field drops the window rather than reporting it as zero.
	bad := http.Header{}
	bad.Set("x-codex-primary-used-percent", "not a number")
	if p := PlanFromHeaders(bad); p != nil {
		t.Fatalf("unparseable percent = %#v", p)
	}

	full := http.Header{}
	full.Set("x-codex-primary-used-percent", "12.5")
	full.Set("x-codex-primary-window-minutes", "300")
	full.Set("x-codex-primary-reset-at", "1704069000")
	full.Set("x-codex-secondary-used-percent", "44")
	full.Set("x-codex-secondary-window-minutes", "10080")
	full.Set("x-codex-credits-has-credits", "true")
	full.Set("x-codex-credits-unlimited", "false")
	full.Set("x-codex-credits-balance", "$5.00")
	p = PlanFromHeaders(full)
	if p.Primary.UsedPercent != 12.5 || p.Primary.WindowMinutes != 300 || p.Primary.ResetsAt != 1704069000 {
		t.Fatalf("primary = %#v", p.Primary)
	}
	if p.Secondary.WindowMinutes != 10080 {
		t.Fatalf("secondary = %#v", p.Secondary)
	}
	if p.Credits == nil || !p.Credits.HasCredits || p.Credits.Unlimited || p.Credits.Balance != "$5.00" {
		t.Fatalf("credits = %#v", p.Credits)
	}
	// A window with no length keeps 0: like the endpoint's zero, that means the
	// plan disabled the window, not that it lasts forever.
	short := http.Header{}
	short.Set("x-codex-primary-used-percent", "1")
	if w := PlanFromHeaders(short).Primary; w.WindowMinutes != 0 {
		t.Fatalf("missing window length = %#v", w)
	}
}

func TestPlanFromUsagePayload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want *PlanUsage
	}{
		{
			name: "empty object has no quota to report",
			body: `{}`,
		},
		{
			name: "primary and secondary windows",
			body: `{"rate_limit":{"primary_window":{"used_percent":42,"limit_window_seconds":300,"reset_at":7},
			        "secondary_window":{"used_percent":84,"limit_window_seconds":3600}}}`,
			want: &PlanUsage{
				// Seconds are rounded up into minutes.
				Primary:   &Window{UsedPercent: 42, WindowMinutes: 5, ResetsAt: 7},
				Secondary: &Window{UsedPercent: 84, WindowMinutes: 60},
			},
		},
		{
			name: "a disabled window is reported as zero minutes",
			body: `{"rate_limit":{"primary_window":{"used_percent":0,"limit_window_seconds":0}}}`,
			want: &PlanUsage{Primary: &Window{UsedPercent: 0}},
		},
		{
			name: "credits without a usable balance",
			body: `{"credits":{"has_credits":false,"unlimited":false,"balance":"0"}}`,
			want: &PlanUsage{Credits: &Credits{}},
		},
		{
			name: "unlimited credits carry no balance",
			body: `{"credits":{"has_credits":true,"unlimited":true,"balance":"9.99"}}`,
			want: &PlanUsage{Credits: &Credits{HasCredits: true, Unlimited: true, Balance: "9.99"}},
		},
		{
			name: "a window without a percent is not a window",
			body: `{"rate_limit":{"primary_window":{"limit_window_seconds":300}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var raw usageResponse
			if err := json.Unmarshal([]byte(tc.body), &raw); err != nil {
				t.Fatal(err)
			}
			got := raw.plan()
			if tc.want == nil {
				if !got.Empty() {
					t.Fatalf("got %#v, want empty", got)
				}
				return
			}
			if got.Empty() {
				t.Fatalf("got an empty plan, want %#v", tc.want)
			}
			assertPlan(t, got, tc.want)
		})
	}
}

// assertPlan compares the reported quota field by field.
func assertPlan(t *testing.T, got, want *PlanUsage) {
	t.Helper()
	assertWindow(t, "primary", got.Primary, want.Primary)
	assertWindow(t, "secondary", got.Secondary, want.Secondary)
	switch {
	case want.Credits == nil:
		if got.Credits != nil {
			t.Fatalf("credits = %#v, want none", got.Credits)
		}
	case got.Credits == nil:
		t.Fatalf("credits = nil, want %#v", want.Credits)
	default:
		if *got.Credits != *want.Credits {
			t.Fatalf("credits = %#v, want %#v", got.Credits, want.Credits)
		}
	}
}

func assertWindow(t *testing.T, name string, got, want *Window) {
	t.Helper()
	switch {
	case want == nil:
		if got != nil {
			t.Fatalf("%s = %#v, want none", name, got)
		}
	case got == nil:
		t.Fatalf("%s = nil, want %#v", name, want)
	default:
		if *got != *want {
			t.Fatalf("%s = %#v, want %#v", name, got, want)
		}
	}
}

func TestFetchPlanUsage(t *testing.T) {
	var gotAuth, gotAccount, gotOriginator string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		gotOriginator = r.Header.Get("originator")
		_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":7,"limit_window_seconds":18000,"reset_at":1704069000}}}`))
	}))
	t.Cleanup(srv.Close)
	prev := UsageURL
	UsageURL = srv.URL
	t.Cleanup(func() { UsageURL = prev })

	plan, err := FetchPlanUsage(context.Background(), "token-1", "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer token-1" || gotAccount != "acct-1" || gotOriginator == "" {
		t.Fatalf("auth=%q account=%q originator=%q", gotAuth, gotAccount, gotOriginator)
	}
	if plan.Primary == nil || plan.Primary.UsedPercent != 7 || plan.Primary.WindowMinutes != 300 {
		t.Fatalf("plan = %#v", plan)
	}
	// No account id (an odd credential) still sends the request.
	if _, err := FetchPlanUsage(context.Background(), "token-1", ""); err != nil {
		t.Fatal(err)
	}
	if gotAccount != "" {
		t.Fatalf("empty account id must not be sent: %q", gotAccount)
	}

	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail":"nope"}`))
	}))
	t.Cleanup(srvErr.Close)
	UsageURL = srvErr.URL
	if _, err := FetchPlanUsage(context.Background(), "t", "a"); err == nil {
		t.Fatal("expected status error")
	}

	UsageURL = srv.URL + "/x"
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{`))
	}))
	t.Cleanup(srvBad.Close)
	UsageURL = srvBad.URL
	if _, err := FetchPlanUsage(context.Background(), "t", "a"); err == nil {
		t.Fatal("expected parse error")
	}

	UsageURL = "http://127.0.0.1:1"
	if _, err := FetchPlanUsage(context.Background(), "t", "a"); err == nil {
		t.Fatal("expected transport error")
	}
}
