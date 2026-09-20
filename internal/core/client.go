package core

import (
	"context"
	"strings"

	"github.com/axispx/zeta/internal/ai"
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
