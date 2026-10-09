package compact

import (
	"context"
	"fmt"

	"github.com/axispx/zeta/internal/ai"
)

// RetainedUserTokens bounds the user messages kept verbatim beside a native
// checkpoint (est. tokens). The newest one always stays.
const RetainedUserTokens = 20_000

// elidedOutput replaces a tool result left out of a compaction request.
const elidedOutput = "[output omitted: too large to compact]"

// NativeCompleter has the provider compact a request itself (*ai.Client for
// providers that support it; see ai.Client.NativeCompaction).
type NativeCompleter interface {
	CompactNative(ctx context.Context, msgs []ai.Message, tools []ai.Tool) (ai.Compaction, ai.Usage, error)
}

// NativeHistory is the history after a native compaction: the user messages
// worth keeping verbatim, then the provider's checkpoint. Everything else —
// assistant text, tool calls and results — is what the checkpoint stands for.
//
// The session log rebuilds history through this too, from the records before
// the compaction, so it must stay a pure function of before and c.
func NativeHistory(before []ai.Message, c ai.Compaction) []ai.Message {
	out := RetainUsers(before)
	return append(out, ai.Message{Role: ai.RoleCompaction, Compaction: &c})
}

// RetainUsers returns the newest user messages within RetainedUserTokens, oldest
// first. A compaction checkpoint is not a user message in this sense: whatever
// it said is already in the provider's new one.
func RetainUsers(history []ai.Message) []ai.Message {
	var kept []ai.Message
	tokens := 0
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role != ai.RoleUser || IsCheckpoint(m) {
			continue
		}
		cost := estimateMsg(m)
		if len(kept) > 0 && tokens+cost > RetainedUserTokens {
			break
		}
		tokens += cost
		kept = append(kept, m)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept
}

// RunNative has the provider compact history. force skips the budget check
// (manual /compact). It is a live-shaped request — cfg.Prefix, then the
// history — so the provider serves it from its prompt cache.
//
// A history already past the window cannot be sent as is, so tool results are
// elided oldest-first, in the copy that goes out only: the durable history is
// not touched, and the request is the only thing that misses the cache.
//
// A result that is not smaller than what it replaces is discarded.
func RunNative(ctx context.Context, c NativeCompleter, history []ai.Message, cfg Config, force bool) (Result, error) {
	if c == nil {
		return Result{}, fmt.Errorf("compact: nil completer")
	}
	if len(history) == 0 || (!force && !overBudget(history, cfg)) {
		return Result{History: history}, nil
	}

	req := make([]ai.Message, 0, len(cfg.Prefix.Messages)+len(history))
	req = append(req, cfg.Prefix.Messages...)
	req = append(req, fitToWindow(history, cfg.ContextWindow, Estimate(cfg.Prefix.Messages))...)

	item, usage, err := c.CompactNative(ctx, req, cfg.Prefix.Tools)
	if err != nil {
		return Result{}, fmt.Errorf("compact: %w", err)
	}
	if item.Content == "" {
		return Result{}, fmt.Errorf("compact: empty checkpoint")
	}

	out := NativeHistory(history, item)
	if Estimate(out) >= Estimate(history) {
		return Result{History: history, Usage: usage}, nil
	}
	return Result{History: out, Compacted: true, Native: &item, Usage: usage}, nil
}

// fitToWindow returns history, or a copy of it with the oldest large tool
// results elided until the request is estimated to fit window. Estimates are
// chars/4 and the request carries tool definitions, so it aims under the window.
func fitToWindow(history []ai.Message, window, prefixTokens int) []ai.Message {
	if window <= 0 {
		return history
	}
	target := window - window/8 - DefaultToolsOverhead
	total := prefixTokens + Estimate(history)
	if total <= target {
		return history
	}
	out := append([]ai.Message(nil), history...)
	elided := estimateMsg(ai.Message{Role: ai.RoleTool, Text: elidedOutput})
	for i := range out {
		if total <= target {
			break
		}
		m := out[i]
		if m.Role != ai.RoleTool {
			continue
		}
		cost := estimateMsg(m)
		if cost <= elided {
			continue
		}
		m.Text = elidedOutput
		out[i] = m
		total -= cost - elided
	}
	return out
}
