package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/axispx/zeta/internal/todo"
)

// Todo is the tool name for the session checklist.
const Todo = "todo"

type todoTool struct {
	store *todo.Store
}

func (todoTool) Name() string { return Todo }

func (todoTool) Description() string {
	return "Track multi-step work with a session-scoped checklist. " +
		"Use for non-trivial tasks; skip one-liner requests. " +
		"Mark a single item in_progress before starting it and completed when done. " +
		"Statuses: pending, in_progress, completed, cancelled. " +
		"Each call fully replaces the list — always send the full intended set " +
		"(empty items clears). " +
		"The harness reinjects the current list each turn as a developer message " +
		"(# Session todos with - [ ] / - [~] / - [x] / - [!] marks)."
}

func (todoTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "Stable item id (required)",
						},
						"subject": map[string]any{
							"type":        "string",
							"description": "Short label (required)",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "Optional longer detail",
						},
						"status": map[string]any{
							"type":        "string",
							"description": "pending | in_progress | completed | cancelled",
							"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
						},
					},
					"required": []string{"id", "subject"},
				},
				"description": "Full todo list. Empty array clears the list.",
			},
		},
		"required": []string{"items"},
	}
}

func (t todoTool) Summary(raw json.RawMessage) string {
	var a struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(raw, &a)
	return fmt.Sprintf("%s %d items", Todo, len(a.Items))
}

func (t todoTool) Run(_ context.Context, _ string, raw json.RawMessage) (string, error) {
	if t.store == nil {
		return "", fmt.Errorf("todo store unavailable")
	}
	items, err := todo.ParseArgs(raw)
	if err != nil {
		return "", err
	}
	warn, err := t.store.Replace(items)
	if err != nil {
		return "", err
	}
	out := t.store.Format()
	if warn != "" {
		out = out + "\n" + warn
	}
	return out, nil
}
