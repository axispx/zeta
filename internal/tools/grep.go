package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type grepTool struct{}

func (grepTool) Name() string { return "grep" }
func (grepTool) Description() string {
	return "Search the workspace for a regex. Returns matching lines with file:line prefixes."
}
func (grepTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression to search for",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Subdirectory or file to search (optional, default workspace root)",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Glob filter, e.g. \"*.go\" (optional)",
			},
		},
		"required": []string{"pattern"},
	}
}

func (grepTool) Summary(raw json.RawMessage) string {
	var a grepArgs
	_ = json.Unmarshal(raw, &a)
	parts := []string{"grep", strconv.Quote(a.Pattern)}
	if p := strings.TrimSpace(a.Path); p != "" {
		parts = append(parts, p)
	}
	if g := strings.TrimSpace(a.Glob); g != "" {
		parts = append(parts, "--glob "+g)
	}
	return strings.Join(parts, " ")
}

type grepArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
}

func (grepTool) Run(ctx context.Context, root string, raw json.RawMessage) (string, error) {
	var args grepArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return "", fmt.Errorf("pattern is required")
	}

	abs, err := resolveSearchPath(root, args.Path)
	if err != nil {
		return "", err
	}
	target, err := rgTarget(root, abs)
	if err != nil {
		return "", err
	}

	cmdArgs := append([]string{"--line-number", "--no-heading", "--color", "never"}, rgWorkspaceFlags()...)
	if g := strings.TrimSpace(args.Glob); g != "" {
		cmdArgs = append(cmdArgs, "--glob", g)
	}
	cmdArgs = append(cmdArgs, "--", args.Pattern)
	if target != "" {
		cmdArgs = append(cmdArgs, target)
	}

	stdout, err := runRg(ctx, root, cmdArgs)
	if err != nil {
		return "", err
	}
	lines := linesFromRg(stdout)
	if len(lines) == 0 {
		return "no matches", nil
	}
	truncated := false
	if len(lines) > maxGrepLines {
		lines = lines[:maxGrepLines]
		truncated = true
	}
	joined := strings.Join(lines, "\n")
	if len(joined) > maxGrepBytes {
		joined = joined[:maxGrepBytes]
		truncated = true
	}
	if truncated {
		joined += fmt.Sprintf(truncNoteGrep, maxGrepLines)
	}
	return joined, nil
}
