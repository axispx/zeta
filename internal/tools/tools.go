package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/todo"
)

// Tool is one function the model may call.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Summary(args json.RawMessage) string
	Run(ctx context.Context, root string, args json.RawMessage) (string, error)
}

// ArgPath returns the "path" JSON argument for edit/write-style tools, or "".
func ArgPath(raw json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &a)
	return strings.TrimSpace(a.Path)
}

// Build returns the full tool set (build mode). Todo runs only when a store is wired;
// harness paths should prefer BuildWith.
func Build() []Tool { return BuildWith(nil) }

// BuildWith returns the full tool set with todo bound to store (nil store → tool errors on Run).
func BuildWith(store *todo.Store) []Tool {
	return []Tool{readTool{}, editTool{}, writeTool{}, grepTool{}, globTool{}, bashTool{}, websearchTool{}, webfetchTool{}, skillTool{}, todoTool{store: store}, askUserTool{}}
}

// Inspect returns ask/plan-safe tools (no edits, no shell). Prefer InspectWith in the harness.
func Inspect() []Tool { return InspectWith(nil) }

// InspectWith returns ask/plan-safe tools with todo bound to store.
func InspectWith(store *todo.Store) []Tool {
	return []Tool{readTool{}, grepTool{}, globTool{}, websearchTool{}, webfetchTool{}, skillTool{}, todoTool{store: store}, askUserTool{}}
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

// interactiveTool is implemented by tools that never auto-run: the harness
// must supply a Reply (Deny / Inject) before the agent loop continues.
// Side-effect tools use permission.NeedsDecision separately.
type interactiveTool interface {
	Interactive() bool
}

// Interactive reports whether a named tool requires a harness decision and
// never runs via Tool.Run on its own. Looks up the tool in Build().
func Interactive(name string) bool {
	t, ok := ByName(Build(), name)
	if !ok {
		return false
	}
	it, ok := t.(interactiveTool)
	return ok && it.Interactive()
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
	return limitToolOutput(out)
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
	// Per-tool capture limits only (silent). Model-facing size/line policy is limitToolOutput.
	maxReadBytes   = 200 * 1024
	maxReadLines   = 2000
	maxGrepLines   = 200
	maxGlobResults = 500
)
