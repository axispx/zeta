package core

import (
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/skill"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

// ToolsForMode is the tool set a turn runs with: read-only in ask/plan, full in
// every other mode.
func ToolsForMode(mode prompt.Mode, store *todo.Store) []tools.Tool {
	env := tools.Env{Todos: store}
	switch mode {
	case prompt.ModeAsk, prompt.ModePlan:
		return tools.ForMode(false, env)
	default:
		return tools.ForMode(true, env)
	}
}

// RequestPrefix is the byte-stable head of every request in a session: the
// system prompt and mode instructions. It heads the prefix providers cache, so
// it is also what compaction reuses. Nothing volatile belongs here.
func RequestPrefix(ws workspace.Context, mode prompt.Mode) []ai.Message {
	return []ai.Message{
		{Role: ai.RoleSystem, Text: prompt.System(ws)},
		{Role: ai.RoleDeveloper, Text: mode.Instructions()},
	}
}

// RequestMsgs prepends system + mode instructions to the durable history and
// appends the per-request developer blocks: a trailing slash-skill playbook
// (invoking turn only), the environment, and the todo checklist.
//
// Only those trailing blocks may differ between requests. Providers cache the
// request prefix, so the system prompt and the durable history have to stay
// byte-identical for the cache to hit; anything injected ahead of them would
// invalidate the whole transcript whenever it moved. The tail is ordered by
// volatility, most stable first: environment changes on checkout or day
// rollover, the todos block on most tool turns.
func RequestMsgs(ws workspace.Context, mode prompt.Mode, history []ai.Message, todos *todo.Store) []ai.Message {
	out := make([]ai.Message, 0, len(history)+5)
	out = append(out, RequestPrefix(ws, mode)...)
	// Durable history keeps the user text (token + optional args); completed
	// slash turns are not re-injected on later requests.
	out = append(out, history...)
	if n := len(history); n > 0 {
		last := history[n-1]
		if last.Role == ai.RoleUser {
			if s, ok := skill.MatchSlash(last.Text); ok {
				out = append(out, ai.Message{Role: ai.RoleDeveloper, Text: skill.SlashInjection(s)})
			}
		}
	}
	out = append(out, ai.Message{Role: ai.RoleDeveloper, Text: prompt.Environment(ws)})

	if todos != nil {
		if block := todos.PromptBlock(); block != "" {
			out = append(out, ai.Message{Role: ai.RoleDeveloper, Text: block})
		}
	}
	return out
}
