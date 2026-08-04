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

func TestTokenRequestInvalidGrant(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	t.Cleanup(srv.Close)

	_, err := tokenRequest(context.Background(), srv.URL, url.Values{})
	if !errors.Is(err, ErrInvalidGrant) {
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

func TestStartDeviceUnsupported(t *testing.T) {
	t.Parallel()
	_, err := StartDevice(context.Background(), "openai")
	if err == nil {
		t.Fatal("expected error")
	}
}
