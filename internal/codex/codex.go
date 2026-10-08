// Package codex describes the ChatGPT Codex backend: the Responses API host a
// ChatGPT plan (rather than a platform API key) is entitled to talk to, and
// the model catalog that comes with it.
//
// It depends only on oauth (the client identity the backend gates on) and
// paths, because config (the connect preset), ai (the transport) and the TUI
// (model discovery) all need these facts.
package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axispx/zeta/internal/oauth"
	"github.com/axispx/zeta/internal/paths"
)

const (
	// ProviderID is the provider id the ChatGPT sign-in is stored under: the same
	// "openai" provider as the API-key path, with a different base URL and models.
	ProviderID = "openai"
	// DisplayName is the provider's label in /config.
	DisplayName = "OpenAI"
	// BaseURL is the Codex backend, reachable only with a ChatGPT-plan token:
	// an api.openai.com key does not work here.
	BaseURL = "https://chatgpt.com/backend-api/codex"
	// ResponsesPath is the streaming completion endpoint under BaseURL.
	ResponsesPath = "/responses"
	// ModelsPath lists the models the signed-in account may use.
	ModelsPath = "/models"

	// codexHost and codexBasePath identify the backend in a provider's base URL.
	codexHost     = "chatgpt.com"
	codexBasePath = "/backend-api/codex"

	// defaultContextWindow covers catalog entries that omit a window (the
	// backend only sends one for models it has metadata for).
	defaultContextWindow = 272_000

	// originator identifies zeta to the backend, which gates ChatGPT-plan
	// usage on the client identity alongside the token.
	originator = oauth.CodexOriginator

	// clientVersion is the Codex CLI release zeta presents to the models
	// endpoint, which rejects a request without one (400, "client_version
	// Field required") and filters the catalog by it.
	clientVersion = "0.160.1"

	cacheFile   = "codex-models.json"
	httpTimeout = 20 * time.Second
	userAgent   = "zeta"
)

// ModelsURL is the account's model catalog endpoint (var so tests can
// redirect).
var ModelsURL = BaseURL + ModelsPath

// IsEndpoint reports whether baseURL points at the Codex backend, which speaks
// the Responses API rather than Chat Completions.
func IsEndpoint(baseURL string) bool {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host != codexHost {
		return false
	}
	return strings.HasPrefix(strings.TrimRight(u.Path, "/"), codexBasePath)
}

// Model is one model the backend offers the signed-in account.
type Model struct {
	Slug          string
	Name          string
	ContextWindow int
	// Efforts are the reasoning_effort values the model accepts.
	Efforts []string
	// FastTier is the service_tier id that turns on the model's fast mode
	// (higher speed, more plan usage). Empty means the model has none.
	FastTier string
}

// Models lists the account's models from the backend. The list is
// entitlement-gated server-side, which is why it is discovered rather than
// bundled: plan memberships differ, and slugs come and go.
func Models(ctx context.Context, accessToken, accountID string) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ModelsURL, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("client_version", clientVersion)
	req.URL.RawQuery = q.Encode()
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
		return nil, fmt.Errorf("codex models: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("codex models: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("codex models: %s (status %d)", strings.TrimSpace(string(body)), resp.StatusCode)
	}

	var raw modelsResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("codex models: parse: %w", err)
	}
	return raw.models(), nil
}

type modelsResponse struct {
	Models []struct {
		Slug          string `json:"slug"`
		DisplayName   string `json:"display_name"`
		Visibility    string `json:"visibility"`
		ContextWindow int    `json:"context_window"`
		// MaxContextWindow backs context_window when the latter is absent.
		MaxContextWindow         int `json:"max_context_window"`
		SupportedReasoningLevels []struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
		ServiceTiers []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"service_tiers"`
	} `json:"models"`
}

// models reduces the raw catalog to selectable entries: models the backend does
// not list for the picker (hidden/experimental) are dropped.
func (r modelsResponse) models() []Model {
	out := make([]Model, 0, len(r.Models))
	seen := map[string]bool{}
	for _, m := range r.Models {
		slug := strings.TrimSpace(m.Slug)
		if slug == "" || seen[slug] || (m.Visibility != "" && m.Visibility != "list") {
			continue
		}
		seen[slug] = true
		name := strings.TrimSpace(m.DisplayName)
		if name == "" {
			name = slug
		}
		ctx := m.ContextWindow
		if ctx <= 0 {
			ctx = m.MaxContextWindow
		}
		if ctx <= 0 {
			ctx = defaultContextWindow
		}
		var efforts []string
		for _, l := range m.SupportedReasoningLevels {
			if e := strings.TrimSpace(l.Effort); e != "" {
				efforts = append(efforts, e)
			}
		}
		var fast string
		for _, t := range m.ServiceTiers {
			if strings.EqualFold(strings.TrimSpace(t.Name), "fast") {
				fast = strings.TrimSpace(t.ID)
				break
			}
		}
		out = append(out, Model{Slug: slug, Name: name, ContextWindow: ctx, Efforts: efforts, FastTier: fast})
	}
	return out
}

// CachePath returns $ZETA_HOME/cache/codex-models.json.
func CachePath() string {
	home := paths.Home()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "cache", cacheFile)
}

// CachedModels returns the catalog from the last successful discovery, or nil.
// Model discovery needs a live token, so the cache is the only list available
// to /config between sign-ins.
func CachedModels() []Model {
	path := CachePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var models []Model
	if err := json.Unmarshal(data, &models); err != nil {
		return nil
	}
	return models
}

// SaveModels records a discovery result. Best effort: a cache miss only costs
// a list until the next discovery, so callers may ignore the error.
func SaveModels(models []Model) error {
	path := CachePath()
	if path == "" || len(models) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(models)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
