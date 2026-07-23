package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestSupports(t *testing.T) {
	t.Parallel()
	if !Supports("xai") {
		t.Fatal("expected xai supported")
	}
	if Supports("openai") || Supports("") {
		t.Fatal("unexpected support")
	}
}

func TestGeneratePKCE(t *testing.T) {
	t.Parallel()
	a, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if a.Verifier == "" || a.Challenge == "" || a.Method != "S256" {
		t.Fatalf("got %#v", a)
	}
	if a.Verifier == b.Verifier || a.Challenge == b.Challenge {
		t.Fatal("expected unique PKCE codes")
	}
}

func TestGenerateState(t *testing.T) {
	t.Parallel()
	a, b := GenerateState(), GenerateState()
	if a == "" || a == b {
		t.Fatalf("a=%q b=%q", a, b)
	}
}

func TestTokenRequestPendingErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	}))
	t.Cleanup(srv.Close)

	_, err := tokenRequest(context.Background(), srv.URL, url.Values{})
	if !errors.Is(err, ErrAuthorizationPending) {
		t.Fatalf("got %v", err)
	}
}

func TestTokenRequestSlowDown(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
	}))
	t.Cleanup(srv.Close)

	_, err := tokenRequest(context.Background(), srv.URL, url.Values{})
	if !errors.Is(err, ErrSlowDown) {
		t.Fatalf("got %v", err)
	}
}

func TestRefresh(t *testing.T) {
	t.Parallel()
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "access",
			RefreshToken: "refresh2",
			ExpiresIn:    3600,
			TokenType:    "bearer",
		})
	}))
	t.Cleanup(srv.Close)

	prev := XaiTokenURL
	XaiTokenURL = srv.URL
	t.Cleanup(func() { XaiTokenURL = prev })

	tok, err := Refresh(context.Background(), "xai", "refresh1")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "access" || tok.RefreshToken != "refresh2" {
		t.Fatalf("got %#v", tok)
	}
	vals, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("grant_type") != "refresh_token" ||
		vals.Get("refresh_token") != "refresh1" ||
		vals.Get("client_id") != XaiClientID {
		t.Fatalf("body=%q parsed=%v", gotBody, vals)
	}

	_, err = Refresh(context.Background(), "openai", "r")
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestBrowserFlowPasteCode(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") != "pasted-code" {
			t.Errorf("code = %q", r.Form.Get("code"))
		}
		if r.Form.Get("redirect_uri") != XaiRedirectURI {
			t.Errorf("redirect_uri = %q", r.Form.Get("redirect_uri"))
		}
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "from-paste",
			ExpiresIn:   3600,
			TokenType:   "bearer",
		})
	}))
	t.Cleanup(tokenSrv.Close)
	prev := XaiTokenURL
	XaiTokenURL = tokenSrv.URL
	t.Cleanup(func() { XaiTokenURL = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	paste := make(chan string, 1)
	openBrowser := func(string) error {
		paste <- "pasted-code"
		return nil
	}

	tok, err := BrowserFlow(ctx, "xai", BrowserFlowOptions{
		OpenBrowser: openBrowser,
		Paste:       paste,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "from-paste" {
		t.Fatalf("got %#v", tok)
	}
}

func TestParseAuthPaste(t *testing.T) {
	t.Parallel()
	code, state, err := ParseAuthPaste("  abcDEF123  ")
	if err != nil || code != "abcDEF123" || state != "" {
		t.Fatalf("raw: code=%q state=%q err=%v", code, state, err)
	}
	code, state, err = ParseAuthPaste("http://127.0.0.1:56121/callback?code=xyz&state=s1")
	if err != nil || code != "xyz" || state != "s1" {
		t.Fatalf("url: code=%q state=%q err=%v", code, state, err)
	}
	_, _, err = ParseAuthPaste("")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStartDeviceUnsupported(t *testing.T) {
	t.Parallel()
	_, err := StartDevice(context.Background(), "openai")
	if err == nil {
		t.Fatal("expected error")
	}
}
