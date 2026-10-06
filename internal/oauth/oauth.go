// Package oauth implements the interactive OAuth 2.0 logins zeta supports
// (xAI device code, ChatGPT/Codex loopback browser) plus the token endpoints
// they share.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const userAgent = "zeta/oauth"

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Device-code polling signals, produced by any token endpoint zeta polls.
var (
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrSlowDown             = errors.New("slow_down")
	// ErrInvalidGrant means the refresh token was rejected/consumed and the
	// user must sign in again.
	ErrInvalidGrant = errors.New("oauth grant rejected")
)

// TokenResponse is the OAuth token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	TokenType    string `json:"token_type"` // "bearer"
	Scope        string `json:"scope,omitempty"`
	// IDToken is the OIDC id_token, when the provider issues one (codex).
	IDToken string `json:"id_token,omitempty"`

	// AccountID is the account the tokens are scoped to, parsed out of the
	// id_token's claims. Codex only: it is sent as chatgpt-account-id.
	AccountID string `json:"-"`

	Error string `json:"error,omitempty"`
}

func tokenRequest(ctx context.Context, tokenURL string, form url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	if tok.Error != "" {
		switch tok.Error {
		case "authorization_pending":
			return nil, ErrAuthorizationPending
		case "slow_down":
			return nil, ErrSlowDown
		case "invalid_grant":
			return nil, ErrInvalidGrant
		default:
			return nil, fmt.Errorf("token endpoint error: %s", tok.Error)
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint: %s (status %d)", string(body), resp.StatusCode)
	}

	if tok.AccessToken == "" {
		return nil, fmt.Errorf("token response: empty access_token")
	}

	return &tok, nil
}

func xaiRefresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {XaiClientID},
	}
	return tokenRequest(ctx, XaiTokenURL, form)
}

func xaiStartDevice(ctx context.Context) (DeviceCode, error) {
	form := url.Values{
		"client_id": {XaiClientID},
		"scope":     {XaiScope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, XaiDeviceAuthorizeURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return DeviceCode{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return DeviceCode{}, fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return DeviceCode{}, fmt.Errorf("read device code response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DeviceCode{}, fmt.Errorf("device code: %s (status %d)", string(body), resp.StatusCode)
	}

	var raw deviceCodeResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return DeviceCode{}, fmt.Errorf("parse device code: %w", err)
	}
	if raw.DeviceCode == "" {
		return DeviceCode{}, fmt.Errorf("device code: empty device_code in response")
	}
	return raw.toDeviceCode(), nil
}

func xaiPollDevice(ctx context.Context, device DeviceCode) (*TokenResponse, error) {
	interval := time.Duration(device.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)

	pollForm := url.Values{
		"grant_type":  {XaiDeviceGrantType},
		"device_code": {device.DeviceCode},
		"client_id":   {XaiClientID},
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		tok, err := tokenRequest(ctx, XaiTokenURL, pollForm)
		if err != nil {
			if errors.Is(err, ErrAuthorizationPending) {
				continue
			}
			if errors.Is(err, ErrSlowDown) {
				interval += 5 * time.Second
				continue
			}
			return nil, err
		}
		return tok, nil
	}

	return nil, fmt.Errorf("device code expired")
}
