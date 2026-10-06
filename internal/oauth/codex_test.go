package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// testJWT builds an unsigned token with the given claims. zeta only decodes
// the payload, so the signature segment is a placeholder.
func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(payload) + ".sig"
}

// codexLoginServer serves the token endpoint for a ChatGPT login. The
// response carries no expires_in — the access token's own exp is the source.
func codexLoginServer(t *testing.T, accessToken, idToken string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q", got)
		}
		if r.Form.Get("code") != "the-code" {
			t.Errorf("code = %q", r.Form.Get("code"))
		}
		if r.Form.Get("code_verifier") == "" {
			t.Error("missing code_verifier")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken,
			"refresh_token": "refresh",
			"id_token":      idToken,
			"token_type":    "bearer",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCodexAuthorizeURL(t *testing.T) {
	t.Parallel()
	got := codexAuthorizeURL("http://localhost:1455/auth/callback", "challenge", "state")
	if !strings.HasPrefix(got, CodexAuthorizeURL+"?") {
		t.Fatalf("authorize URL = %q", got)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	want := map[string]string{
		"response_type":              "code",
		"client_id":                  CodexClientID,
		"redirect_uri":               "http://localhost:1455/auth/callback",
		"scope":                      CodexScope,
		"code_challenge":             "challenge",
		"code_challenge_method":      "S256",
		"state":                      "state",
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"originator":                 CodexOriginator,
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Fatalf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

// TestCodexBrowserLogin runs the flow end to end against a local callback and
// a stub token endpoint.
func TestCodexBrowserLogin(t *testing.T) {
	idToken := testJWT(t, map[string]any{
		"exp": time.Now().Add(time.Hour).Unix(),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-1",
		},
	})
	accessToken := testJWT(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	srv := codexLoginServer(t, accessToken, idToken)
	prevURL, prevPort := CodexTokenURL, CodexRedirectPort
	CodexTokenURL, CodexRedirectPort = srv.URL, 0
	t.Cleanup(func() { CodexTokenURL, CodexRedirectPort = prevURL, prevPort })

	flow, err := codexBeginBrowser()
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close()

	authorize, err := url.Parse(flow.URL())
	if err != nil {
		t.Fatal(err)
	}
	if flow.UserCode() != "" {
		t.Fatalf("browser flow has no user code, got %q", flow.UserCode())
	}
	redirect := authorize.Query().Get("redirect_uri")
	if !strings.HasPrefix(redirect, "http://localhost:") {
		t.Fatalf("redirect_uri = %q", redirect)
	}

	done := make(chan *TokenResponse, 1)
	go func() {
		tok, err := flow.Wait(context.Background())
		if err != nil {
			t.Errorf("wait: %v", err)
		}
		done <- tok
	}()

	resp, err := http.Get(redirect + "?code=the-code&state=" + url.QueryEscape(authorize.Query().Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d", resp.StatusCode)
	}

	select {
	case tok := <-done:
		if tok == nil {
			t.Fatal("no token")
		}
		if tok.AccessToken != accessToken || tok.RefreshToken != "refresh" {
			t.Fatalf("token = %#v", tok)
		}
		if tok.AccountID != "acct-1" {
			t.Fatalf("account id = %q", tok.AccountID)
		}
		// No expires_in in the response: the access token's exp fills it in.
		if tok.ExpiresIn < 3500 || tok.ExpiresIn > 3600 {
			t.Fatalf("expires_in = %d, want ~3600", tok.ExpiresIn)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("login did not finish")
	}
}

func TestCodexCallbackRejectsBadState(t *testing.T) {
	prev := CodexRedirectPort
	CodexRedirectPort = 0
	t.Cleanup(func() { CodexRedirectPort = prev })

	flow, err := codexBeginBrowser()
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close()
	f := flow.(*codexBrowserFlow)

	rec := httptest.NewRecorder()
	f.handleCallback(rec, httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state=other", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	select {
	case res := <-f.results:
		t.Fatalf("stale callback delivered %#v", res)
	default:
	}
}

func TestCodexWaitCancelledByClose(t *testing.T) {
	prev := CodexRedirectPort
	CodexRedirectPort = 0
	t.Cleanup(func() { CodexRedirectPort = prev })

	flow, err := codexBeginBrowser()
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := flow.Wait(context.Background())
		errc <- err
	}()
	time.Sleep(20 * time.Millisecond)
	flow.Close()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not unblock Wait")
	}
}

func TestCodexRefresh(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.Form
		fmt.Fprint(w, `{"access_token":"access2","expires_in":3600,"token_type":"bearer"}`)
	}))
	t.Cleanup(srv.Close)
	prev := CodexTokenURL
	CodexTokenURL = srv.URL
	t.Cleanup(func() { CodexTokenURL = prev })

	tok, err := Refresh(context.Background(), "openai", "refresh1")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "access2" || tok.ExpiresIn != 3600 {
		t.Fatalf("token = %#v", tok)
	}
	// A refresh response without id_token leaves the account id to the stored
	// credential (empty here, so empty is expected — not an error).
	if tok.AccountID != "" {
		t.Fatalf("unexpected account id %q", tok.AccountID)
	}
	if gotForm.Get("grant_type") != "refresh_token" || gotForm.Get("refresh_token") != "refresh1" ||
		gotForm.Get("client_id") != CodexClientID {
		t.Fatalf("form = %v", gotForm)
	}
	if gotForm.Get("scope") != "" {
		t.Fatalf("refresh must not narrow scopes: %v", gotForm)
	}
}

func TestJWTAccountIDAndExpiry(t *testing.T) {
	t.Parallel()
	nested := testJWT(t, map[string]any{
		"exp":                         time.Now().Add(2 * time.Hour).Unix(),
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-nested"},
	})
	if got := jwtAccountID(nested); got != "acct-nested" {
		t.Fatalf("nested account id = %q", got)
	}
	if got := jwtExpiresIn(nested); got < 7000 || got > 7200 {
		t.Fatalf("expires_in = %d, want ~7200", got)
	}

	flat := testJWT(t, map[string]any{"chatgpt_account_id": "acct-flat"})
	if got := jwtAccountID(flat); got != "acct-flat" {
		t.Fatalf("flat account id = %q", got)
	}

	expired := testJWT(t, map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})
	if got := jwtExpiresIn(expired); got != 0 {
		t.Fatalf("expired token lifetime = %d, want 0", got)
	}
	if got := jwtAccountID("not-a-jwt"); got != "" {
		t.Fatalf("garbage token account id = %q", got)
	}
}
