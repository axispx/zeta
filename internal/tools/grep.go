package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type grepTool struct{}

func (grepTool) Name() string        { return "grep" }
func (grepTool) Access() Access      { return AccessRead }
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
	var a struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	_ = json.Unmarshal(raw, &a)
	parts := []string{"grep", strconv.Quote(a.Pattern)}
	if a.Path != "" {
		parts = append(parts, a.Path)
	}
	if a.Glob != "" {
		parts = append(parts, "--glob "+a.Glob)
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
	if _, err := exec.LookPath("rg"); err != nil {
		return "", fmt.Errorf("rg (ripgrep) not found on PATH; install with: brew install ripgrep")
	}

	searchPath := root
	if strings.TrimSpace(args.Path) != "" {
		abs, err := resolvePath(root, args.Path)
		if err != nil {
			return "", err
		}
		searchPath = abs
	}

	cmdArgs := []string{"--line-number", "--no-heading", "--color", "never", "--hidden", "--glob", "!.git"}
	if args.Glob != "" {
		cmdArgs = append(cmdArgs, "--glob", args.Glob)
	}
	cmdArgs = append(cmdArgs, "--", args.Pattern, searchPath)

	cmd := exec.CommandContext(ctx, "rg", cmdArgs...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// rg exits 1 when no matches.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "no matches", nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}

	out := relativizeGrep(root, stdout.String())
	lines := strings.Split(out, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
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
	if joined == "" {
		return "no matches", nil
	}
	return joined, nil
}

func relativizeGrep(root, out string) string {
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	var b strings.Builder
	for i, line := range strings.Split(out, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimPrefix(line, prefix))
	}
	return b.String()
}
