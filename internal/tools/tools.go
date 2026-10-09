package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// ArgReason returns the "reason" JSON argument, or "". A read outside the
// workspace must carry one: it is the justification the approval prompt shows.
func ArgReason(raw json.RawMessage) string {
	var a struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(raw, &a)
	return strings.TrimSpace(a.Reason)
}

// ArgCommand returns the "command" JSON argument for bash, or "".
func ArgCommand(raw json.RawMessage) string {
	var a struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &a)
	return strings.TrimSpace(a.Command)
}

// ArgWorkdir returns the "workdir" JSON argument for bash, or "".
func ArgWorkdir(raw json.RawMessage) string {
	var a struct {
		Workdir string `json:"workdir"`
	}
	_ = json.Unmarshal(raw, &a)
	return strings.TrimSpace(a.Workdir)
}

// ArgPrefixRule returns the "prefix_rule" JSON argument for bash, or nil. It is
// the prefix the model proposed to remember; whether it may be offered is the
// policy's decision (Policy.RequestedRule), not this accessor's.
func ArgPrefixRule(raw json.RawMessage) []string {
	var a struct {
		PrefixRule []string `json:"prefix_rule"`
	}
	_ = json.Unmarshal(raw, &a)
	return a.PrefixRule
}

// Env carries session-scoped tool dependencies the harness wires per turn.
type Env struct {
	Todos *todo.Store
}

// Build returns the full tool set with no session env.
func Build() []Tool { return For(Env{}) }

// For returns the full tool set. env.Todos binds the session checklist
// (nil → todo tool errors on Run).
func For(env Env) []Tool {
	return []Tool{readTool{}, editTool{}, writeTool{}, grepTool{}, globTool{}, bashTool{}, websearchTool{}, webfetchTool{}, skillTool{}, todoTool{store: env.Todos}, askUserTool{}}
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
// Side-effect tools are classified separately by permission.Classify.
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
			return fmt.Sprintf("error: tool %q is not available", name)
		}
		return fmt.Sprintf("error: unknown tool %q", name)
	}
	out, err := t.Run(ctx, root, args)
	if err != nil {
		return "error: " + err.Error()
	}
	// edit/write diffs stay full so humans can review every line (permission
	// preview, transcript, session +/-). Other tools still hit the budget.
	if name == Edit || name == Write {
		return out
	}
	return limitToolOutput(out)
}

// resolvePath joins root with a relative path and returns the cleaned absolute
// path, the path relative to root using '/' separators (empty when it escapes),
// and whether it escapes the workspace root. Escapes are allowed; the caller
// decides how to gate them (see resolveConfinedPath for search tools).
func resolvePath(root, path string) (abs, rel string, outside bool, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", false, fmt.Errorf("path is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", false, err
	}
	path = expandHome(path)
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(rootAbs, path))
	}
	rel, err = filepath.Rel(rootAbs, abs)
	if err != nil {
		// Different volumes / non-comparable roots: treat as outside.
		return abs, "", true, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs, "", true, nil
	}
	return abs, filepath.ToSlash(rel), false, nil
}

// expandHome replaces a leading "~" or "~/" with the user's home directory, so
// "~/notes" names the home folder rather than a literal "~" under the workspace.
// Other forms (e.g. "~user/") and a missing home directory are returned as given.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// resolveConfinedPath is resolvePath plus a workspace-escape rejection. Used by
// tools that must stay inside the workspace (grep/glob targets, bash workdir).
func resolveConfinedPath(root, path string) (string, error) {
	abs, _, outside, err := resolvePath(root, path)
	if err != nil {
		return "", err
	}
	if outside {
		return "", fmt.Errorf("path %q is outside the workspace", path)
	}
	return abs, nil
}

// ResolveTarget resolves a read/edit/write path argument against root. abs is
// the cleaned absolute path (empty when argPath cannot be resolved). rel is
// workspace-relative with '/' separators (empty when it escapes). outside
// reports an escape. One resolution serves the approval prompt's
// "(outside workspace)" mark and the path a permission rule would remember.
func ResolveTarget(root, argPath string) (abs, rel string, outside bool) {
	abs, rel, outside, err := resolvePath(root, argPath)
	if err != nil {
		return "", "", false
	}
	return abs, rel, outside
}

// EditTarget is ResolveTarget without the absolute path. Kept for edit/write
// permission matching, which never remembers or matches an escaped target.
func EditTarget(root, argPath string) (rel string, outside bool) {
	_, rel, outside = ResolveTarget(root, argPath)
	return rel, outside
}

// ExternalDir is the outside-workspace approval boundary for abs: the path
// itself when it is a directory, otherwise its parent. Empty abs returns "".
func ExternalDir(abs string) string {
	abs = filepath.Clean(abs)
	if abs == "" || abs == "." {
		return ""
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return abs
	}
	return filepath.Dir(abs)
}

// displayPath returns path relative to root when possible.
func displayPath(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

// DisplayTarget is the path a transcript row shows for an edit/write target:
// workspace-relative inside the workspace, absolute (home as ~) outside it, so
// two files that share a basename stay distinguishable and an escape is visible.
// An unresolvable path is returned as given.
func DisplayTarget(root, argPath string) string {
	abs, rel, outside := ResolveTarget(root, argPath)
	switch {
	case abs == "":
		return argPath
	case !outside:
		return rel
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if abs == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(abs, home+string(filepath.Separator)); ok {
			return "~/" + filepath.ToSlash(rest)
		}
	}
	return abs
}

const (
	// Per-tool capture limits only (silent). Model-facing size/line policy is limitToolOutput.
	maxReadBytes   = 200 * 1024
	maxReadLines   = 2000
	maxGrepLines   = 200
	maxGlobResults = 500
)
