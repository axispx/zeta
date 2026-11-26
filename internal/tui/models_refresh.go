package tui

import (
	"context"
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/models"
)

const modelsRefreshTimeout = 20 * time.Second

// modelsRefreshedMsg carries the catalogs read in the background at startup.
type modelsRefreshedMsg struct {
	presets map[string]config.Preset // models.dev, by provider id
	codex   []codex.Model            // nil when discovery was skipped or failed
}

// refreshModelsCmd re-reads the model catalogs so configured providers pick up
// new models, context windows and reasoning levels without a visit to /config.
// It works on a clone: Update must not share the live config with a goroutine.
// A stale ChatGPT token is refreshed first; the rotated pair is written to disk.
// Failures are silent — the saved list keeps working.
func refreshModelsCmd(cfg config.Config) tea.Cmd {
	var wantCatalog bool
	var codexID string
	for id, p := range cfg.Providers {
		switch {
		case p.Custom:
		case codex.IsEndpoint(p.BaseURL):
			if p.OAuth != nil {
				codexID = id
			}
		default:
			wantCatalog = true
		}
	}
	if !wantCatalog && codexID == "" {
		return nil
	}
	return func() tea.Msg {
		var out modelsRefreshedMsg
		if wantCatalog {
			raw, err := models.Load()
			if err != nil && len(raw) == 0 {
				return nil
			}
			out.presets = make(map[string]config.Preset)
			for _, pre := range config.PresetsFromModels(raw) {
				out.presets[pre.ID] = pre
			}
		}
		if codexID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), modelsRefreshTimeout)
			defer cancel()
			// The access token is short-lived, so it is usually stale by the
			// next launch. Refreshing persists the new pair to disk; the live
			// config adopts it on its next request.
			if _, err := cfg.EnsureOAuthFresh(ctx, codexID); err == nil {
				p, _ := cfg.Provider(codexID)
				if list, err := codex.Models(ctx, p.AuthToken(), p.OAuth.AccountID); err == nil && len(list) > 0 {
					_ = codex.SaveModels(list)
					out.codex = list
				}
			}
		}
		return out
	}
}

// handleModelsRefreshed merges the fresh catalogs into the live config and
// saves it when something changed. A model removed upstream stays listed while
// it is the active one, so the session never loses the model it is talking to.
func (m *Model) handleModelsRefreshed(msg modelsRefreshedMsg) {
	cfg := &m.session.Cfg
	active, hasActive := cfg.ActiveChoice()
	changed := false
	for id, p := range cfg.Providers {
		if p.Custom {
			continue
		}
		var pre config.Preset
		var ok bool
		if codex.IsEndpoint(p.BaseURL) {
			if msg.codex == nil {
				continue
			}
			pre, ok = config.CodexPreset(msg.codex), true
		} else {
			pre, ok = msg.presets[id]
		}
		if !ok || len(pre.Models) == 0 {
			continue
		}
		if hasActive && active.ProviderID == id {
			if _, listed := pre.Models[active.ModelID]; !listed {
				pre.Models[active.ModelID] = p.Models[active.ModelID]
			}
		}
		if err := cfg.SyncCatalogModels(id, pre.Models); err != nil {
			continue
		}
		if next, _ := cfg.Provider(id); !reflect.DeepEqual(p.Models, next.Models) {
			changed = true
		}
	}
	if changed {
		if err := cfg.Save(); err != nil {
			m.noteError("model refresh: " + err.Error())
		}
	}
}
