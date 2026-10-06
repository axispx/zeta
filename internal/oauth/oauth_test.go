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

func TestKindFor(t *testing.T) {
	t.Parallel()
	cases := map[string]Kind{
		"xai":    KindDeviceCode,
		"openai": KindBrowser,
		"codex":  KindNone,
		"":       KindNone,
	}
	for id, want := range cases {
		if got := KindFor(id); got != want {
			t.Fatalf("KindFor(%q) = %v, want %v", id, got, want)
		}
		if got := Supports(id); got != (want != KindNone) {
			t.Fatalf("Supports(%q) = %v", id, got)
		}
	}
	if Browser("xai") || !Browser("openai") {
		t.Fatal("Browser must be true for openai only")
	}
}

func TestRotatesRefreshToken(t *testing.T) {
	t.Parallel()
	if !RotatesRefreshToken("xai") {
		t.Fatal("xai rotates its refresh token")
	}
	if RotatesRefreshToken("openai") {
		t.Fatal("openai keeps its refresh token")
	}
}

func TestTokenRequestErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"pending", http.StatusOK, `{"error":"authorization_pending"}`, ErrAuthorizationPending},
		{"slow down", http.StatusOK, `{"error":"slow_down"}`, ErrSlowDown},
		{"invalid grant", http.StatusBadRequest, `{"error":"invalid_grant"}`, ErrInvalidGrant},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)

			_, err := tokenRequest(context.Background(), srv.URL, url.Values{})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
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

	_, err = Refresh(context.Background(), "deepseek", "r")
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestBeginUnsupported(t *testing.T) {
	t.Parallel()
	if _, err := Begin(context.Background(), "deepseek"); err == nil {
		t.Fatal("expected error")
	}
}
