package config

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/axispx/zeta/internal/oauth"
)

const oauthRefreshSkewMs = 5 * 60 * 1000 // refresh when within 5 minutes of expiry

// AuthToken returns the bearer credential: OAuth access token if set, else API key.
// Providers should keep these mutually exclusive (ConnectPreset / ConnectPresetOAuth /
// UpdateProvider clear the other side).
func (p Provider) AuthToken() string {
	if p.OAuth != nil {
		if t := strings.TrimSpace(p.OAuth.AccessToken); t != "" {
			return t
		}
	}
	return strings.TrimSpace(p.APIKey)
}

// HasUsableCredential reports whether p has an API key or an OAuth access token.
func (p Provider) HasUsableCredential() bool {
	return p.AuthToken() != ""
}

// OAuthFromToken maps a token endpoint response into stored credentials.
func OAuthFromToken(tok *oauth.TokenResponse) *OAuthCredential {
	if tok == nil || strings.TrimSpace(tok.AccessToken) == "" {
		return nil
	}
	oc := &OAuthCredential{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + tok.ExpiresIn*1000,
		TokenType:    tok.TokenType,
	}
	return oc
}

// ApplyToken updates stored OAuth credentials from a refresh/token response.
func (oc *OAuthCredential) ApplyToken(tok *oauth.TokenResponse) {
	if oc == nil || tok == nil {
		return
	}
	oc.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		oc.RefreshToken = tok.RefreshToken
	}
	oc.ExpiresAt = time.Now().UnixMilli() + tok.ExpiresIn*1000
	if tok.TokenType != "" {
		oc.TokenType = tok.TokenType
	}
}

// RefreshOAuthIfNeeded refreshes p's OAuth tokens when near expiry.
// Returns true when p was mutated.
func RefreshOAuthIfNeeded(ctx context.Context, providerID string, p *Provider) (bool, error) {
	if p == nil || p.OAuth == nil || strings.TrimSpace(p.OAuth.RefreshToken) == "" {
		return false, nil
	}
	now := time.Now().UnixMilli()
	if p.OAuth.ExpiresAt > now+oauthRefreshSkewMs {
		return false, nil
	}
	tok, err := oauth.Refresh(ctx, providerID, p.OAuth.RefreshToken)
	if err != nil {
		return false, fmt.Errorf("token refresh: %w", err)
	}
	p.OAuth.ApplyToken(tok)
	return true, nil
}

// EnsureOAuthFresh refreshes and persists OAuth tokens for providerID when needed.
// Returns true when the provider credential changed.
func (c *Config) EnsureOAuthFresh(ctx context.Context, providerID string) (bool, error) {
	p, ok := c.Provider(providerID)
	if !ok {
		return false, nil
	}
	refreshed, err := RefreshOAuthIfNeeded(ctx, providerID, &p)
	if err != nil || !refreshed {
		return refreshed, err
	}
	if err := c.PutProvider(providerID, p); err != nil {
		return false, err
	}
	if err := c.Save(); err != nil {
		return false, err
	}
	return true, nil
}
