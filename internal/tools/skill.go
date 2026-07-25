package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/axispx/zeta/internal/skill"
)

// Skill is the tool name for loading a bundled playbook.
const Skill = "skill"

type skillTool struct{}

func (skillTool) Name() string { return Skill }

func (skillTool) Description() string {
	return "Load a bundled skill when the task matches one of the available skills in the system context. " +
		"Injects the skill's full instructions into the conversation. " +
		"The name must match an entry from the available skills list."
}

func (skillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "The name of the skill from the available skills list",
			},
		},
		"required": []string{"name"},
	}
}

func (skillTool) Summary(raw json.RawMessage) string {
	var a struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &a)
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return Skill
	}
	return Skill + " " + name
}

func (skillTool) Run(_ context.Context, _ string, raw json.RawMessage) (string, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	s, ok := skill.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown skill %q", name)
	}
	return skill.FormatContent(s), nil
}
