package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type editTool struct{}

func (editTool) Name() string { return "edit" }
func (editTool) Description() string {
	return "Edit a file by replacing a unique old_string with new_string. " +
		"If old_string is empty and the file does not exist, create it with new_string. " +
		"Set replace_all to true to replace every occurrence."
}
func (editTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path relative to the workspace root",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "Exact text to find. Empty + missing file creates a new file.",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "Replacement text (or full contents when creating)",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace every occurrence of old_string (default false)",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (editTool) Summary(raw json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &a)
	if a.Path != "" {
		return "edit " + a.Path
	}
	return "edit"
}

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func (editTool) Run(ctx context.Context, root string, raw json.RawMessage) (string, error) {
	_ = ctx
	var args editArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	abs, err := resolvePath(root, args.Path)
	if err != nil {
		return "", err
	}
	rel := displayPath(root, abs)

	_, statErr := os.Stat(abs)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}

	// Create new file.
	if args.OldString == "" {
		if exists {
			return "", fmt.Errorf("%s already exists; provide old_string to edit it", rel)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(abs, []byte(args.NewString), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("created %s (%d bytes)", rel, len(args.NewString)), nil
	}

	if !exists {
		return "", fmt.Errorf("%s does not exist", rel)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	content := string(data)
	count := strings.Count(content, args.OldString)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", rel)
	}
	if count > 1 && !args.ReplaceAll {
		return "", fmt.Errorf("old_string found %d times in %s; make it unique or set replace_all", count, rel)
	}

	var next string
	n := count
	if args.ReplaceAll {
		next = strings.ReplaceAll(content, args.OldString, args.NewString)
	} else {
		next = strings.Replace(content, args.OldString, args.NewString, 1)
		n = 1
	}
	if err := os.WriteFile(abs, []byte(next), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("edited %s (%d replacement%s)", rel, n, plural(n)), nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
