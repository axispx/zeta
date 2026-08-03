package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/axispx/zeta/internal/rg"
)

type globTool struct{}

// Glob is the tool name for filename pattern search.
const Glob = "glob"

func (globTool) Name() string { return Glob }
func (globTool) Description() string {
	return "Find files under the workspace by glob pattern. " +
		"Uses the same ignore rules as grep (gitignore, etc.). " +
		"Patterns use / separators (e.g. \"**/*.go\", \"internal/**/*.ts\"). " +
		"A pattern without / matches basenames anywhere (e.g. \"*.go\")."
}
func (globTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern (e.g. \"**/*.go\")",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Subdirectory to search under (optional, default workspace root)",
			},
		},
		"required": []string{"pattern"},
	}
}

func (globTool) Summary(raw json.RawMessage) string {
	var a globArgs
	_ = json.Unmarshal(raw, &a)
	parts := []string{Glob}
	if p := strings.TrimSpace(a.Pattern); p != "" {
		parts = append(parts, p)
	}
	if p := strings.TrimSpace(a.Path); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func (globTool) Run(ctx context.Context, root string, raw json.RawMessage) (string, error) {
	var args globArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	abs, err := resolveSearchDir(root, args.Path)
	if err != nil {
		return "", err
	}
	target, err := rgTarget(root, abs)
	if err != nil {
		return "", err
	}

	cmdArgs := append([]string{"--files", "--sort=path"}, rg.WorkspaceFlags()...)
	cmdArgs = append(cmdArgs, "--glob", pattern)
	if target != "" {
		cmdArgs = append(cmdArgs, "--", target)
	}

	stdout, err := rg.Run(ctx, root, cmdArgs)
	if err != nil {
		return "", err
	}

	matches := rg.SlashPaths(rg.Lines(stdout))
	if len(matches) == 0 {
		return "no matches", nil
	}
	// Silent capture cap; model-facing size/line policy is limitToolOutput.
	if len(matches) > maxGlobResults {
		matches = matches[:maxGlobResults]
	}
	return strings.Join(matches, "\n"), nil
}
