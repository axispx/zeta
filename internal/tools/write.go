package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

type writeTool struct{}

func (writeTool) Name() string { return "write" }
func (writeTool) Description() string {
	return "Write full contents to a file (create or overwrite). " +
		"Prefer edit for surgical changes; use write for new files with known contents " +
		"or intentional full-file replacement (including empty files). " +
		"On success, returns a unified diff of the change (empty if unchanged)."
}
func (writeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path relative to the workspace root",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full file contents to write",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (writeTool) Summary(raw json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &a)
	if a.Path != "" {
		return "write " + a.Path
	}
	return "write"
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func planWrite(root string, args writeArgs) (fileChange, error) {
	abs, err := resolvePath(root, args.Path)
	if err != nil {
		return fileChange{}, err
	}
	rel := displayPath(root, abs)
	var before string
	data, err := os.ReadFile(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return fileChange{}, err
		}
	} else {
		before = string(data)
	}
	return fileChange{abs: abs, rel: rel, before: before, after: args.Content}, nil
}

func (writeTool) Run(ctx context.Context, root string, raw json.RawMessage) (string, error) {
	_ = ctx
	var args writeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	chg, err := planWrite(root, args)
	if err != nil {
		return "", err
	}
	if err := applyFileChange(chg); err != nil {
		return "", err
	}
	return unifiedDiff(chg.rel, chg.before, chg.after), nil
}

func (writeTool) Preview(root string, raw json.RawMessage) string {
	var args writeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "write"
	}
	chg, err := planWrite(root, args)
	return diffPreview(chg, err, writeTool{}.Summary(raw))
}
