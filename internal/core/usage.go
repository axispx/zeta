package core

import (
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/session"
)

// ModelUsage is one model's share of a session's token accounting.
type ModelUsage struct {
	Name       string // display name; "" when the provider/model is unknown
	Input      int64
	Output     int64
	Total      int64
	Cached     int64
	CacheWrite int64
	Responses  int
}

// Usage is provider-reported token accounting totalled over a session's
// assistant turns, for /usage.
//
// It is deliberately cumulative across /model switches rather than reset: every
// turn was billed, so dropping a switch's spend would under-report the session.
// Turns are bucketed by the model that produced them, which is also the only
// honest way to read cache numbers — each model has its own prompt cache, so a
// hit rate carried across a switch describes a cache that no longer exists.
// The footer's contextTokens reset on switch for that reason; this does not.
//
// It is also not the footer's context footprint: that is a single response's
// fill toward the model window, while this accumulates every billed call and
// survives /compact (compaction rewrites history but not the transcript).
// Summarizer and title calls are not counted — those completers report no
// usage.
type Usage struct {
	Models        []*ModelUsage // first-seen order
	byName        map[string]*ModelUsage
	Input         int64
	Output        int64
	Total         int64
	Cached        int64
	CacheWrite    int64
	Responses     int
	CacheReported bool // at least one turn reported cache accounting
}

// bucket returns (creating if needed) the per-model bucket for name.
func (u *Usage) bucket(name string) *ModelUsage {
	if u.byName == nil {
		u.byName = make(map[string]*ModelUsage, 2)
	}
	if m, ok := u.byName[name]; ok {
		return m
	}
	m := &ModelUsage{Name: name}
	u.byName[name] = m
	u.Models = append(u.Models, m)
	return m
}

// Add folds one response's usage into the running totals, attributed to model.
// Responses with no token counts (providers that omit usage) are ignored rather
// than counted as a zero-token turn.
func (u *Usage) Add(model string, v ai.Usage) {
	total := v.ContextTokens()
	if total <= 0 {
		return
	}
	u.Responses++
	u.Input += v.PromptTokens
	u.Output += v.CompletionTokens
	u.Total += total
	u.Cached += v.CachedTokens
	u.CacheWrite += v.CacheWriteTokens
	u.CacheReported = u.CacheReported || v.CacheReported

	m := u.bucket(model)
	m.Responses++
	m.Input += v.PromptTokens
	m.Output += v.CompletionTokens
	m.Total += total
	m.Cached += v.CachedTokens
	m.CacheWrite += v.CacheWriteTokens
}

// Empty reports whether nothing has been totalled yet.
func (u Usage) Empty() bool { return u.Responses == 0 }

// UsageOrNil returns a turn's accounting for persistence, or nil when the
// provider reported no token counts, so those records stay clean.
func UsageOrNil(u ai.Usage) *ai.Usage {
	if u.ContextTokens() <= 0 {
		return nil
	}
	return &u
}

// UsageFromRecords totals the usage persisted on a session's records, so a
// /resume shows the same numbers as the run that produced them.
func UsageFromRecords(recs []session.Record) Usage {
	var u Usage
	for _, r := range recs {
		if r.Usage != nil {
			u.Add(r.Model, *r.Usage)
		}
	}
	return u
}
