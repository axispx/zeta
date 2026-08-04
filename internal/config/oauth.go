package config

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/axispx/zeta/internal/oauth"
)

const (
	oauthRefreshSkewMs = 5 * 60 * 1000 // refresh when within 5 minutes of expiry
	// Fallback access-token TTL when the server omits expires_in (xAI issues 6h tokens).
	oauthDefaultTTLMs = 6 * 60 * 60 * 1000
)

// ErrReauthRequired reports that the stored refresh token was rejected
// (invalid_grant) and the user must sign in again.
var ErrReauthRequired = errors.New("OAuth session expired — sign in again with /config")

// oauthRefreshMu single-flights refresh+persist in this process so concurrent
// callers cannot redeem the same single-use refresh token twice. Cross-process
// races are resolved by re-reading disk under the file lock after HTTP (the
// lock is never held across the network call).
var oauthRefreshMu sync.Mutex

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
// Requires both access_token and refresh_token: xAI rotates the refresh token
// on every use, so a session without one cannot be renewed.
func OAuthFromToken(tok *oauth.TokenResponse) *OAuthCredential {
	if tok == nil || strings.TrimSpace(tok.AccessToken) == "" {
		return nil
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return nil
	}
	return &OAuthCredential{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    expiresAtFromToken(tok),
		TokenType:    tok.TokenType,
	}
}

// ApplyToken updates stored OAuth credentials from a refresh/token response.
// Callers that require rotation must reject a missing refresh_token before
// calling; an omitted refresh_token here keeps the previous value for
// providers that do not rotate.
func (oc *OAuthCredential) ApplyToken(tok *oauth.TokenResponse) {
	if oc == nil || tok == nil {
		return
	}
	if t := strings.TrimSpace(tok.AccessToken); t != "" {
		oc.AccessToken = t
	}
	if t := strings.TrimSpace(tok.RefreshToken); t != "" {
		oc.RefreshToken = t
	}
	if exp := expiresAtFromToken(tok); exp > 0 {
		oc.ExpiresAt = exp
	}
	if tok.TokenType != "" {
		oc.TokenType = tok.TokenType
	}
	oc.RefreshFailed = false
}

// EnsureOAuthFresh proactively refreshes providerID when the access token is
// near expiry. No-ops when expiry is unknown (ExpiresAt == 0) so a single-use
// refresh token is not burned on every request — use RecoverOAuth after a 401.
//
// Returns true when the in-memory credential changed.
func (c *Config) EnsureOAuthFresh(ctx context.Context, providerID string) (bool, error) {
	return c.refreshOAuth(ctx, providerID, refreshProactive)
}

// RecoverOAuth refreshes providerID after a provider 401. Prefers a fresher
// pair already on disk (another zeta may have rotated); otherwise redeems the
// refresh token. Returns true when the in-memory credential changed.
func (c *Config) RecoverOAuth(ctx context.Context, providerID string) (bool, error) {
	return c.refreshOAuth(ctx, providerID, refreshRecover)
}

type refreshMode int

const (
	refreshProactive refreshMode = iota
	refreshRecover
)

// refreshOAuth is the shared adopt → (HTTP) → commit path. The config file
// lock is held only around disk reads/writes, never across the token request.
func (c *Config) refreshOAuth(ctx context.Context, providerID string, mode refreshMode) (bool, error) {
	if c == nil {
		return false, nil
	}
	path := Path()
	if path == "" {
		return false, fmt.Errorf("cannot resolve config path")
	}

	oauthRefreshMu.Lock()
	defer oauthRefreshMu.Unlock()

	// --- critical section 1: adopt disk, decide whether to hit the network ---
	var (
		changed  bool
		needHTTP bool
		rt       string
	)
	err := withConfigFileLock(path, func() error {
		p, ok := c.Provider(providerID)
		if !ok {
			return nil
		}
		prevAT := oauthAccess(p)

		disk, err := readConfigFile(path)
		if err != nil {
			return err
		}
		if dp, ok := disk.Providers[providerID]; ok && adoptDiskOAuth(&p, dp) {
			c.Providers[providerID] = p
			changed = true
			// Recover: another process already rotated — use their token.
			if mode == refreshRecover && oauthAccess(p) != prevAT && !oauthDead(p) {
				return nil
			}
		}

		if oauthDead(p) {
			return ErrReauthRequired
		}
		rt = oauthRefreshToken(p)
		if rt == "" {
			return nil
		}
		if mode == refreshProactive && !oauthNeedsRefresh(p.OAuth) {
			return nil
		}
		needHTTP = true
		return nil
	})
	if err != nil || !needHTTP {
		return changed, err
	}

	// --- network (no locks) ---
	tok, refreshErr := oauth.Refresh(ctx, providerID, rt)
	if refreshErr != nil && !errors.Is(refreshErr, oauth.ErrInvalidGrant) {
		refreshErr = fmt.Errorf("token refresh: %w", refreshErr)
	}

	// --- critical section 2: re-adopt, commit or mark dead ---
	err = withConfigFileLock(path, func() error {
		p, ok := c.Provider(providerID)
		if !ok {
			return nil
		}
		disk, err := readConfigFile(path)
		if err != nil {
			return err
		}
		if dp, ok := disk.Providers[providerID]; ok && adoptDiskOAuth(&p, dp) {
			c.Providers[providerID] = p
			changed = true
			// Disk moved past the RT we redeemed — take the winner, drop our result.
			if oauthRefreshToken(p) != "" && oauthRefreshToken(p) != rt && !oauthDead(p) {
				return nil
			}
		}

		if refreshErr != nil {
			if errors.Is(refreshErr, oauth.ErrInvalidGrant) {
				if p.OAuth == nil {
					return ErrReauthRequired
				}
				// Only persist when we newly mark dead (short-circuit leaves flag set).
				if !p.OAuth.RefreshFailed {
					p.OAuth.RefreshFailed = true
					c.Providers[providerID] = p
					if err := writeProviderOAuth(path, providerID, p); err != nil {
						return err
					}
					changed = true
				}
				return ErrReauthRequired
			}
			return refreshErr
		}

		// Success: require rotated refresh_token so we never store a dead RT.
		if strings.TrimSpace(tok.RefreshToken) == "" {
			return fmt.Errorf("token refresh: provider omitted refresh_token; re-authenticate with /config")
		}
		if strings.TrimSpace(tok.AccessToken) == "" {
			return fmt.Errorf("token refresh: empty access_token")
		}
		if p.OAuth == nil {
			p.OAuth = &OAuthCredential{}
		}
		p.OAuth.ApplyToken(tok)
		p.APIKey = ""
		c.Providers[providerID] = p
		if err := writeProviderOAuth(path, providerID, p); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

// oauthNeedsRefresh reports whether oc should be proactively refreshed.
// Unknown expiry (0) skips proactive refresh — RecoverOAuth handles 401s.
func oauthNeedsRefresh(oc *OAuthCredential) bool {
	if oc == nil {
		return false
	}
	if oc.RefreshFailed || strings.TrimSpace(oc.RefreshToken) == "" {
		return false
	}
	if oc.ExpiresAt == 0 {
		return false
	}
	return oc.ExpiresAt <= time.Now().UnixMilli()+oauthRefreshSkewMs
}

func oauthAccess(p Provider) string {
	if p.OAuth == nil {
		return ""
	}
	return p.OAuth.AccessToken
}

func oauthRefreshToken(p Provider) string {
	if p.OAuth == nil {
		return ""
	}
	return strings.TrimSpace(p.OAuth.RefreshToken)
}

func oauthDead(p Provider) bool {
	return p.OAuth != nil && p.OAuth.RefreshFailed
}

// adoptDiskOAuth copies disk's OAuth into mem when disk holds a different,
// living credential (typically a rotated refresh token from another process).
// Returns true when mem was mutated.
func adoptDiskOAuth(mem *Provider, disk Provider) bool {
	if mem == nil || disk.OAuth == nil {
		return false
	}
	d := disk.OAuth
	if strings.TrimSpace(d.AccessToken) == "" {
		return false
	}
	if d.RefreshFailed {
		return false
	}
	if mem.OAuth == nil {
		oc := *d
		mem.OAuth = &oc
		mem.APIKey = ""
		return true
	}
	m := mem.OAuth
	// Only adopt when disk clearly moved past what we hold. Same RT after a
	// local invalid_grant must not clear RefreshFailed.
	switch {
	case d.RefreshToken != "" && d.RefreshToken != m.RefreshToken:
		// Rotated pair we don't have yet.
	case d.AccessToken != m.AccessToken && d.ExpiresAt > m.ExpiresAt:
		// Newer access token with later expiry.
	default:
		return false
	}
	oc := *d
	mem.OAuth = &oc
	mem.APIKey = ""
	return true
}

// writeProviderOAuth patches providerID's OAuth onto the on-disk config and
// writes atomically. Uses a fresh read so concurrent non-OAuth edits survive.
// Caller must hold the config file lock.
func writeProviderOAuth(path, providerID string, p Provider) error {
	disk, err := readConfigFile(path)
	if err != nil {
		return err
	}
	if disk.Providers == nil {
		disk.Providers = map[string]Provider{}
	}
	cur, ok := disk.Providers[providerID]
	if !ok {
		// Provider only exists in memory — write the full in-memory provider.
		disk.Providers[providerID] = p
	} else {
		cur.OAuth = p.OAuth
		if p.OAuth != nil {
			cur.APIKey = ""
		}
		disk.Providers[providerID] = cur
	}
	return disk.saveUnlocked(path)
}

func expiresAtFromToken(tok *oauth.TokenResponse) int64 {
	if tok == nil {
		return 0
	}
	now := time.Now().UnixMilli()
	if tok.ExpiresIn > 0 {
		return now + tok.ExpiresIn*1000
	}
	// Known-good default rather than 0 (0 disables proactive refresh).
	return now + oauthDefaultTTLMs
}
