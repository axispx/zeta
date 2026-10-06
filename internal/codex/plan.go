package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// UsagePath serves the account's plan quota as JSON. The backend sends the same
// numbers as rate-limit headers on a streaming response, so this is only the
// fallback for when they are absent.
const UsagePath = "/usage"

// UsageURL is the quota endpoint (var so tests can redirect).
var UsageURL = BaseURL + UsagePath

// Window is one rolling quota window of a ChatGPT plan.
type Window struct {
	// UsedPercent is 0-100.
	UsedPercent float64
	// WindowMinutes is the window length. Zero means the plan disabled this
	// window, which is not the same as a window of unknown length.
	WindowMinutes int64
	// ResetsAt is unix seconds.
	ResetsAt int64
}

// Credits is the account's extra usage balance, when it has one.
type Credits struct {
	HasCredits bool
	Unlimited  bool
	Balance    string
}

// PlanUsage is the subscription quota backing the account, as the Codex
// backend reports it: ChatGPT plans are metered by rolling windows rather than
// billed per token, so token counts alone do not say what a session cost.
type PlanUsage struct {
	// Primary is the short rolling window (the ~5h window).
	Primary *Window
	// Secondary is the long window (weekly).
	Secondary *Window
	Credits   *Credits
}

// Empty reports whether the backend described no quota at all.
func (p *PlanUsage) Empty() bool {
	return p == nil || (p.Primary == nil && p.Secondary == nil && p.Credits == nil)
}

// PlanFromHeaders reads the rate-limit headers the backend puts on a streaming
// response. It returns nil when the response carries none, which is not an
// error: the quota is then fetched from UsagePath or simply unknown.
func PlanFromHeaders(h http.Header) *PlanUsage {
	p := &PlanUsage{
		Primary:   windowFromHeaders(h, "x-codex-primary"),
		Secondary: windowFromHeaders(h, "x-codex-secondary"),
	}
	if has, ok := headerBool(h, "x-codex-credits-has-credits"); ok {
		c := &Credits{HasCredits: has}
		c.Unlimited, _ = headerBool(h, "x-codex-credits-unlimited")
		c.Balance = strings.TrimSpace(h.Get("x-codex-credits-balance"))
		p.Credits = c
	}
	if p.Empty() {
		return nil
	}
	return p
}

// windowFromHeaders reads one window family. Used-percent gates presence, as in
// the backend's own parser: the other two fields are optional detail.
func windowFromHeaders(h http.Header, prefix string) *Window {
	used, err := strconv.ParseFloat(strings.TrimSpace(h.Get(prefix+"-used-percent")), 64)
	if err != nil {
		return nil
	}
	w := &Window{UsedPercent: used}
	w.WindowMinutes, _ = headerInt(h, prefix+"-window-minutes")
	w.ResetsAt, _ = headerInt(h, prefix+"-reset-at")
	return w
}

func headerBool(h http.Header, name string) (bool, bool) {
	raw := strings.TrimSpace(h.Get(name))
	switch {
	case strings.EqualFold(raw, "true"), raw == "1":
		return true, true
	case strings.EqualFold(raw, "false"), raw == "0":
		return false, true
	}
	return false, false
}

func headerInt(h http.Header, name string) (int64, bool) {
	v, err := strconv.ParseInt(strings.TrimSpace(h.Get(name)), 10, 64)
	return v, err == nil
}

// FetchPlanUsage reads the account's quota from the backend when a response did
// not carry it in headers. Best effort by design — an account whose plan
// reports no quota simply has none to show.
func FetchPlanUsage(ctx context.Context, accessToken, accountID string) (*PlanUsage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UsageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("originator", originator)
	req.Header.Set("User-Agent", userAgent)
	if accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex usage: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("codex usage: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("codex usage: %s (status %d)", strings.TrimSpace(string(body)), resp.StatusCode)
	}

	var raw usageResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("codex usage: parse: %w", err)
	}
	return raw.plan(), nil
}

type usageResponse struct {
	RateLimit *struct {
		PrimaryWindow   *usageWindow `json:"primary_window"`
		SecondaryWindow *usageWindow `json:"secondary_window"`
	} `json:"rate_limit"`
	Credits *struct {
		HasCredits *bool   `json:"has_credits"`
		Unlimited  *bool   `json:"unlimited"`
		Balance    *string `json:"balance"`
	} `json:"credits"`
}

type usageWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds *int64   `json:"limit_window_seconds"`
	ResetAt            *int64   `json:"reset_at"`
}

func (r usageResponse) plan() *PlanUsage {
	p := &PlanUsage{}
	if w := r.RateLimit; w != nil {
		p.Primary = w.PrimaryWindow.window()
		p.Secondary = w.SecondaryWindow.window()
	}
	// A balance is only worth showing when the account actually has credits; a
	// "0" on a subscription-only plan is noise.
	if c := r.Credits; c != nil && c.HasCredits != nil {
		credits := &Credits{HasCredits: *c.HasCredits}
		if c.Unlimited != nil {
			credits.Unlimited = *c.Unlimited
		}
		if credits.HasCredits && c.Balance != nil {
			credits.Balance = strings.TrimSpace(*c.Balance)
		}
		p.Credits = credits
	}
	if p.Empty() {
		return nil
	}
	return p
}

func (w *usageWindow) window() *Window {
	if w == nil || w.UsedPercent == nil {
		return nil
	}
	out := &Window{UsedPercent: *w.UsedPercent}
	// The endpoint reports seconds; the UI sizes windows in minutes.
	if w.LimitWindowSeconds != nil && *w.LimitWindowSeconds > 0 {
		out.WindowMinutes = (*w.LimitWindowSeconds + 59) / 60
	}
	if w.ResetAt != nil {
		out.ResetsAt = *w.ResetAt
	}
	return out
}
