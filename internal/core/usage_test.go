package core

import (
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/session"
)

func TestUsageAddSkipsUnreported(t *testing.T) {
	var u Usage
	u.Add("m", ai.Usage{})
	if !u.Empty() || u.Responses != 0 {
		t.Fatalf("empty usage must not count a turn: %+v", u)
	}
	if len(u.Models) != 0 {
		t.Fatalf("unreported turn must not create a bucket: %+v", u.Models)
	}
	u.Add("m", ai.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120})
	if u.Empty() || u.Responses != 1 {
		t.Fatalf("reported usage must count: %+v", u)
	}
}

func TestUsageTotals(t *testing.T) {
	var u Usage
	u.Add("M1", ai.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		TotalTokens:      1200,
		CachedTokens:     800,
		CacheReported:    true,
	})
	// A provider that omits TotalTokens falls back to prompt+completion.
	u.Add("M1", ai.Usage{
		PromptTokens:     500,
		CompletionTokens: 100,
		CachedTokens:     100,
		CacheWriteTokens: 50,
		CacheReported:    true,
	})
	if u.Responses != 2 {
		t.Fatalf("responses = %d", u.Responses)
	}
	if u.Input != 1500 || u.Output != 300 {
		t.Fatalf("input/output = %d/%d, want 1500/300", u.Input, u.Output)
	}
	if u.Total != 1800 {
		t.Fatalf("total = %d, want 1200+600", u.Total)
	}
	if u.Cached != 900 || !u.CacheReported {
		t.Fatalf("cached = %d reported = %v", u.Cached, u.CacheReported)
	}
	if u.CacheWrite != 50 {
		t.Fatalf("cacheWrite = %d", u.CacheWrite)
	}
	if len(u.Models) != 1 || u.Models[0].Total != 1800 {
		t.Fatalf("model bucket = %+v", u.Models)
	}
}

// Switching models mid-session must not reset the total: every turn was billed.
// It does split the accounting, because cache numbers are per-model.
func TestUsageAccumulatesAcrossModelSwitch(t *testing.T) {
	var u Usage
	u.Add("Model A", ai.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, CachedTokens: 800, CacheReported: true})
	u.Add("Model B", ai.Usage{PromptTokens: 2000, CompletionTokens: 300, TotalTokens: 2300, CachedTokens: 0, CacheReported: true})

	if u.Responses != 2 || u.Total != 3400 || u.Input != 3000 {
		t.Fatalf("switch must not drop spend: %+v", u)
	}
	if len(u.Models) != 2 {
		t.Fatalf("want one bucket per model: %+v", u.Models)
	}
	if u.Models[0].Name != "Model A" || u.Models[1].Name != "Model B" {
		t.Fatalf("order must be first-seen: %+v", u.Models)
	}
	if u.Models[0].Cached != 800 || u.Models[1].Cached != 0 {
		t.Fatalf("cache must stay per-model: %+v", u.Models)
	}
	if u.Models[0].Total+u.Models[1].Total != u.Total {
		t.Fatalf("buckets must sum to the total: %+v", u)
	}
}

func TestUsageFromRecords(t *testing.T) {
	recs := []session.Record{
		{Role: session.RoleUser, Text: "hi"},
		{Role: session.RoleAgent, Text: "a", Model: "M1", Usage: &ai.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}},
		{Role: session.RoleTool, Text: "out"},
		{Role: session.RoleAgent, Text: "b", Model: "M1", Usage: &ai.Usage{PromptTokens: 200, CompletionTokens: 20, TotalTokens: 220}},
		{Role: session.RoleCompact, Text: "summary"},
		{Role: session.RoleAgent, Text: "c", Model: "M2", Usage: &ai.Usage{PromptTokens: 300, CompletionTokens: 30, TotalTokens: 330}},
		{Role: session.RoleAgent, Text: "d"}, // provider reported nothing
	}
	got := UsageFromRecords(recs)
	if got.Responses != 3 || got.Input != 600 || got.Total != 660 {
		t.Fatalf("totals from records = %+v", got)
	}
	byModel := map[string]int64{}
	for _, m := range got.Models {
		byModel[m.Name] = m.Total
	}
	if byModel["M1"] != 330 || byModel["M2"] != 330 {
		t.Fatalf("per-model totals = %+v", byModel)
	}
}

func TestUsageOrNil(t *testing.T) {
	if got := UsageOrNil(ai.Usage{}); got != nil {
		t.Fatalf("unreported usage must not persist: %+v", got)
	}
	got := UsageOrNil(ai.Usage{PromptTokens: 1, CompletionTokens: 1})
	if got == nil || got.PromptTokens != 1 {
		t.Fatalf("got %+v", got)
	}
}
