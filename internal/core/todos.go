package core

import (
	"encoding/json"
	"strings"

	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/tools"
)

// SeedTodos replaces the in-memory checklist (resume / new session). items come
// from TodosFromRecords (already normalized) or nil to clear.
func (s *Session) SeedTodos(items []todo.Item) {
	if s.Todos == nil {
		s.Todos = todo.NewStore()
	}
	// Replace is the only mutation path; soft in_progress warning ignored on hydrate.
	_, _ = s.Todos.Replace(items)
}

// TodosFromRecords returns items from the latest successful todo tool call.
// Success = non-denied tool result whose body is Format output ("Todos (N):…").
// Denied, cancelled, error, and incomplete calls are skipped.
func TodosFromRecords(recs []session.Record) []todo.Item {
	results := make(map[string]session.Record, len(recs))
	for _, r := range recs {
		if r.Role == session.RoleTool && r.ToolCallID != "" {
			results[r.ToolCallID] = r
		}
	}
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.Role != session.RoleAgent {
			continue
		}
		for j := len(r.ToolCalls) - 1; j >= 0; j-- {
			tc := r.ToolCalls[j]
			if tc.Name != tools.Todo {
				continue
			}
			res, ok := results[tc.ID]
			if !ok || res.Denied || !todoResultOK(res.Text) {
				continue
			}
			items, err := todo.ParseArgs(json.RawMessage(tc.Arguments))
			if err != nil {
				continue
			}
			return items
		}
	}
	return nil
}

// todoResultOK reports a successful todo tool body (Format output).
func todoResultOK(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "Todos (")
}
