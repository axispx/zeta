package harness

import (
	"context"
	"strings"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
)

// ApplyClient rebuilds the API client from the active provider and model choice.
func (s *Session) ApplyClient() {
	choice, ok := s.Cfg.ActiveChoice()
	if !ok {
		s.Client = nil
		return
	}
	p, ok := s.Cfg.Provider(choice.ProviderID)
	if !ok {
		s.Client = nil
		return
	}
	s.Client = ai.New(p, choice.ModelID)
}

// EnsureFreshClient refreshes OAuth tokens if needed, then rebuilds the client.
func (s *Session) EnsureFreshClient(ctx context.Context) error {
	choice, ok := s.Cfg.ActiveChoice()
	if !ok {
		return nil
	}
	refreshed, err := s.Cfg.EnsureOAuthFresh(ctx, choice.ProviderID)
	if err != nil {
		return err
	}
	if refreshed {
		s.ApplyClient()
	}
	return nil
}

// Billing describes how a provider's tokens are paid for.
type Billing struct {
	// Provider is the provider's display label.
	Provider string
	// Plan is true when the provider is authenticated with a subscription
	// account (OAuth) rather than an API key, so usage is metered by the plan
	// rather than billed per token.
	Plan bool
}

// BillingOf reports how cfg's active provider bills.
func BillingOf(cfg config.Config) Billing {
	choice, ok := cfg.ActiveChoice()
	if !ok {
		return Billing{}
	}
	p, ok := cfg.Provider(choice.ProviderID)
	if !ok {
		return Billing{}
	}
	return Billing{
		Provider: p.DisplayName(choice.ProviderID),
		Plan:     p.OAuth != nil,
	}
}

// SubscriptionPlan returns cfg's active provider when it meters by
// subscription quota rather than per token: the Codex backend, over OAuth.
func SubscriptionPlan(cfg config.Config) (config.Provider, bool) {
	choice, ok := cfg.ActiveChoice()
	if !ok {
		return config.Provider{}, false
	}
	p, ok := cfg.Provider(choice.ProviderID)
	if !ok || p.OAuth == nil || strings.TrimSpace(p.OAuth.AccessToken) == "" {
		return config.Provider{}, false
	}
	if !codex.IsEndpoint(p.BaseURL) {
		return config.Provider{}, false
	}
	return p, true
}

// PlanQuota fetches the active provider's subscription quota. A provider with
// no quota to report — every per-token provider — yields nil.
//
// This is display-only and never on the token path, so /usage can afford the
// round trip.
func PlanQuota(ctx context.Context, cfg config.Config) (*codex.PlanUsage, error) {
	p, ok := SubscriptionPlan(cfg)
	if !ok {
		return nil, nil
	}
	return codex.FetchPlanUsage(ctx, p.AuthToken(), p.OAuth.AccountID)
}

// CanRetryOAuth reports whether a 401 can be recovered via the active
// provider's refresh token. Dead sessions and API-key providers are skipped.
func (s *Session) CanRetryOAuth() bool {
	choice, ok := s.Cfg.ActiveChoice()
	if !ok {
		return false
	}
	p, ok := s.Cfg.Provider(choice.ProviderID)
	if !ok || p.OAuth == nil || p.OAuth.RefreshFailed {
		return false
	}
	return strings.TrimSpace(p.OAuth.RefreshToken) != ""
}

// InstallOAuth writes oc onto the live config and rebuilds the API client.
func (s *Session) InstallOAuth(providerID string, oc *config.OAuthCredential) {
	if oc == nil {
		return
	}
	p, ok := s.Cfg.Providers[providerID]
	if !ok {
		return
	}
	cpy := *oc
	p.OAuth = &cpy
	p.APIKey = ""
	if s.Cfg.Providers == nil {
		s.Cfg.Providers = map[string]config.Provider{}
	}
	s.Cfg.Providers[providerID] = p
	s.ApplyClient()
}
