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

// BrowserFlowOptions configures BrowserFlow.
type BrowserFlowOptions struct {
	OpenBrowser func(string) error
	Statusf     func(string, ...any)
	// Paste receives the authorization code from the provider's "copy this code"
	// screen (or a full redirect URL / query string containing code=).
	Paste <-chan string
}

// ParseAuthPaste extracts code (and optional state) from a pasted auth code
// or callback URL / query string.
func ParseAuthPaste(s string) (code, state string, err error) {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'")
	if s == "" {
		return "", "", fmt.Errorf("empty paste")
	}
	if strings.Contains(s, "code=") {
		raw := s
		switch {
		case strings.Contains(raw, "://"):
			// full URL
		case strings.HasPrefix(raw, "?"):
			raw = "http://127.0.0.1/callback" + raw
		case strings.HasPrefix(raw, "/"):
			raw = "http://127.0.0.1" + raw
		default:
			raw = "http://127.0.0.1/callback?" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			return "", "", fmt.Errorf("parse paste url: %w", err)
		}
		code = u.Query().Get("code")
		state = u.Query().Get("state")
		if code == "" {
			return "", "", fmt.Errorf("paste url missing code")
		}
		return code, state, nil
	}
	// Raw authorization code from the provider's paste screen.
	if strings.ContainsAny(s, " \t\n\r") {
		return "", "", fmt.Errorf("paste looks like text, not an auth code")
	}
	return s, "", nil
}

// BrowserFlow runs provider browser OAuth + PKCE (paste-code completion).
func BrowserFlow(ctx context.Context, providerID string, opts BrowserFlowOptions) (*TokenResponse, error) {
	switch providerID {
	case "xai":
		return xaiBrowserFlow(ctx, opts)
	default:
		return nil, fmt.Errorf("oauth browser flow not supported for provider %q", providerID)
	}
}

// StartDevice begins RFC 8628 device authorization for providerID.
func StartDevice(ctx context.Context, providerID string) (DeviceCode, error) {
	switch providerID {
	case "xai":
		return xaiStartDevice(ctx)
	default:
		return DeviceCode{}, fmt.Errorf("oauth device flow not supported for provider %q", providerID)
	}
}

// PollDevice waits until the user completes device authorization.
func PollDevice(ctx context.Context, providerID string, device DeviceCode) (*TokenResponse, error) {
	switch providerID {
	case "xai":
		return xaiPollDevice(ctx, device)
	default:
		return nil, fmt.Errorf("oauth device flow not supported for provider %q", providerID)
	}
}

// Refresh exchanges a refresh_token for new tokens.
func Refresh(ctx context.Context, providerID, refreshToken string) (*TokenResponse, error) {
	switch providerID {
	case "xai":
		return xaiRefresh(ctx, refreshToken)
	default:
		return nil, fmt.Errorf("oauth refresh not supported for provider %q", providerID)
	}
}

func xaiBrowserFlow(ctx context.Context, opts BrowserFlowOptions) (*TokenResponse, error) {
	if opts.Paste == nil {
		return nil, fmt.Errorf("paste channel required")
	}
	if opts.OpenBrowser == nil {
		return nil, fmt.Errorf("openBrowser required")
	}

	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	state := GenerateState()
	nonce := GenerateState()

	// plan=generic is required for non-allowlisted clients.
	// redirect_uri must match the registered Grok client even though we don't listen.
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {XaiClientID},
		"redirect_uri":          {XaiRedirectURI},
		"scope":                 {XaiScope},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {pkce.Method},
		"state":                 {state},
		"nonce":                 {nonce},
		"plan":                  {"generic"},
		"referrer":              {"zeta"},
	}
	authURL := XaiAuthorizeURL + "?" + params.Encode()

	if opts.Statusf != nil {
		opts.Statusf("Opening browser…")
	}
	if err := opts.OpenBrowser(authURL); err != nil {
		return nil, fmt.Errorf("open browser: %w\nopen manually: %s", err, authURL)
	}
	if opts.Statusf != nil {
		opts.Statusf("Allow access, then paste the code here")
	}

	var code string
	for {
		select {
		case pasted, ok := <-opts.Paste:
			if !ok {
				return nil, ErrPasteClosed
			}
			c, st, err := ParseAuthPaste(pasted)
			if err != nil {
				if opts.Statusf != nil {
					opts.Statusf("Invalid paste: %v — try again", err)
				}
				continue
			}
			if st != "" && st != state {
				if opts.Statusf != nil {
					opts.Statusf("oauth state mismatch — try again")
				}
				continue
			}
			code = c
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		break
	}

	if opts.Statusf != nil {
		opts.Statusf("Exchanging authorization code…")
	}
	tok, err := exchangeCode(ctx, pkce.Verifier, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	return tok, nil
}

func exchangeCode(ctx context.Context, codeVerifier, code string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {XaiRedirectURI},
		"client_id":     {XaiClientID},
		"code_verifier": {codeVerifier},
	}
	return tokenRequest(ctx, XaiTokenURL, form)
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

	var raw DeviceCodeResponse
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
