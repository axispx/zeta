package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/axispx/zeta/internal/ai"
)

// Tool is one function the model may call.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Summary(args json.RawMessage) string
	Run(ctx context.Context, root string, args json.RawMessage) (string, error)
}

// Build returns the full tool set (build mode).
func Build() []Tool {
	return []Tool{readTool{}, editTool{}, grepTool{}, globTool{}, bashTool{}, websearchTool{}, webfetchTool{}}
}

// Inspect returns ask/plan-safe tools (no edits, no shell).
func Inspect() []Tool {
	return []Tool{readTool{}, grepTool{}, globTool{}, websearchTool{}, webfetchTool{}}
}

// Defs converts tools to API function definitions.
func Defs(ts []Tool) []ai.Tool {
	out := make([]ai.Tool, 0, len(ts))
	for _, t := range ts {
		out = append(out, ai.Tool{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return out
}

// ByName looks up a tool in the set.
func ByName(ts []Tool, name string) (Tool, bool) {
	for _, t := range ts {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

// Run executes a named tool from the set. Failures return an error string
// (not a Go error) so the model can recover.
func Run(ctx context.Context, ts []Tool, root, name string, args json.RawMessage) string {
	t, ok := ByName(ts, name)
	if !ok {
		if _, exists := ByName(Build(), name); exists {
			return fmt.Sprintf("error: tool %q is not available in this mode", name)
		}
		return fmt.Sprintf("error: unknown tool %q", name)
	}
	out, err := t.Run(ctx, root, args)
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}

// resolvePath joins root with a relative path and rejects escapes.
func resolvePath(root, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(rootAbs, path))
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the workspace", path)
	}
	return abs, nil
}

// displayPath returns path relative to root when possible.
func displayPath(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

const (
	maxReadBytes   = 200 * 1024
	maxReadLines   = 2000
	maxGrepLines   = 200
	maxGrepBytes   = 100 * 1024
	maxGlobResults = 500
	truncNoteRead  = "\n\n[truncated: showing first %d lines / %d bytes]"
	truncNoteGrep  = "\n\n[truncated: showing first %d lines]"
	truncNoteGlob  = "\n\n[truncated: showing first %d matches]"
)
