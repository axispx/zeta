package harness

import (
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/skill"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

// TurnTools is the tool set a turn runs with.
func TurnTools(store *todo.Store) []tools.Tool {
	return tools.For(tools.Env{Todos: store})
}

// RequestPrefix is the byte-stable head of every request in a session: the
// system prompt and agent instructions. It heads the prefix providers cache, so
// it is also what compaction reuses. Nothing volatile belongs here.
func RequestPrefix(ws workspace.Context) []ai.Message {
	return []ai.Message{
		{Role: ai.RoleSystem, Text: prompt.System(ws)},
		{Role: ai.RoleDeveloper, Text: prompt.Instructions()},
	}
}

// RequestMsgs prepends system + agent instructions to the durable history and
// appends the per-request developer blocks: a trailing slash-skill playbook
// (invoking turn only), the environment, and the todo checklist.
//
// Only those trailing blocks may differ between requests. Providers cache the
// request prefix, so the system prompt and the durable history have to stay
// byte-identical for the cache to hit; anything injected ahead of them would
// invalidate the whole transcript whenever it moved. The tail is ordered by
// volatility, most stable first: environment changes on checkout or day
// rollover, the todos block on most tool turns.
func RequestMsgs(ws workspace.Context, history []ai.Message, todos *todo.Store) []ai.Message {
	out := make([]ai.Message, 0, len(history)+5)
	out = append(out, RequestPrefix(ws)...)
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
