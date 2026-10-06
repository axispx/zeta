package config

import (
	"strings"

	"github.com/axispx/zeta/internal/codex"
)

// CodexPreset is the connectable preset for the ChatGPT Codex backend. Its
// models come from codex discovery (or codex's fallback list) rather than
// models.dev, because the backend's catalog is gated per ChatGPT plan.
func CodexPreset(models []codex.Model) Preset {
	defs := make(map[string]ModelDef, len(models))
	ids := make([]string, 0, len(models))
	for _, m := range models {
		slug := strings.TrimSpace(m.Slug)
		if slug == "" || m.ContextWindow <= 0 {
			continue
		}
		if _, ok := defs[slug]; ok {
			continue
		}
		defs[slug] = ModelDef{
			Name:             strings.TrimSpace(m.Name),
			ContextWindow:    m.ContextWindow,
			ReasoningEfforts: m.Efforts,
		}
		ids = append(ids, slug)
	}
	pre := Preset{
		ID:      codex.ProviderID,
		Name:    codex.DisplayName,
		BaseURL: codex.BaseURL,
		Models:  defs,
	}
	// Discovery returns models in the backend's own priority order, so the
	// first one is the sensible default for a fresh connect.
	if len(ids) > 0 {
		pre.DefaultModel = ids[0]
	}
	return pre
}
