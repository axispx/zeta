package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Edit is the tool name for surgical file patches.
const Edit = "edit"

type editTool struct{}

func (editTool) Name() string { return Edit }
func (editTool) Description() string {
	return "Edit a file by replacing a unique old_string with new_string. " +
		"If old_string is empty and the file does not exist, create it with new_string. " +
		"For full overwrites of existing files (including empty ones), use write. " +
		"Set replace_all to true to replace every occurrence. " +
		"On success, returns a unified diff of the change (empty if the file was unchanged)."
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
				"type": "string",
				"description": "Exact text to find. Empty + missing file creates a new file. " +
					"Cannot be empty when the file already exists — use write to replace contents.",
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
		Path      string `json:"path"`
		OldString string `json:"old_string"`
	}
	_ = json.Unmarshal(raw, &a)
	verb := Edit
	if a.OldString == "" {
		verb = "create"
	}
	if a.Path != "" {
		return verb + " " + a.Path
	}
	return verb
}

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func planEdit(root string, args editArgs) (fileChange, error) {
	abs, err := resolvePath(root, args.Path)
	if err != nil {
		return fileChange{}, err
	}
	rel := displayPath(root, abs)

	_, statErr := os.Stat(abs)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return fileChange{}, statErr
	}

	if args.OldString == "" {
		if exists {
			return fileChange{}, fmt.Errorf("%s already exists; provide old_string to edit it, or use write to replace contents", rel)
		}
		return fileChange{abs: abs, rel: rel, after: args.NewString}, nil
	}
	if !exists {
		return fileChange{}, fmt.Errorf("%s does not exist", rel)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fileChange{}, err
	}
	content := string(data)
	count := strings.Count(content, args.OldString)
	if count == 0 {
		return fileChange{}, fmt.Errorf("old_string not found in %s", rel)
	}
	if count > 1 && !args.ReplaceAll {
		return fileChange{}, fmt.Errorf("old_string found %d times in %s; make it unique or set replace_all", count, rel)
	}
	var next string
	if args.ReplaceAll {
		next = strings.ReplaceAll(content, args.OldString, args.NewString)
	} else {
		next = strings.Replace(content, args.OldString, args.NewString, 1)
	}
	return fileChange{abs: abs, rel: rel, before: content, after: next}, nil
}

func (editTool) Run(ctx context.Context, root string, raw json.RawMessage) (string, error) {
	_ = ctx
	var args editArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	chg, err := planEdit(root, args)
	if err != nil {
		return "", err
	}
	if err := applyFileChange(chg); err != nil {
		return "", err
	}
	return unifiedDiff(chg.rel, chg.before, chg.after), nil
}

func (editTool) Preview(root string, raw json.RawMessage) string {
	var args editArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Edit
	}
	chg, err := planEdit(root, args)
	return diffPreview(chg, err, editTool{}.Summary(raw))
}
