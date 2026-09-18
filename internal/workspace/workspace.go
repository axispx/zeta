package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// Context holds cwd and git branch for status display.
type Context struct {
	// Abs is the absolute working directory (for session keys, etc.).
	Abs string
	// Cwd is a display path (home replaced with ~).
	Cwd    string
	Branch string
	// AgentsMD is the nearest AGENTS.md within the trust target (cwd → git root,
	// or cwd only when not in a repo). Read at session start (Load) and re-read
	// only at a session boundary — opening or clearing a session, or a
	// compaction — never per turn. Empty when the folder is not trusted.
	AgentsMD string
}

// Load reads cwd, git branch, and AGENTS.md from the current process directory.
// AGENTS.md is loaded only when the trust target is approved; branch/cwd still fill.
func Load() Context {
	abs, err := os.Getwd()
	if err != nil {
		abs = ""
	}
	c := Context{
		Abs:    abs,
		Cwd:    DisplayPath(abs),
		Branch: Branch(abs),
	}
	c.ReloadAgents()
	return c
}

// RefreshBranch re-reads git HEAD for c.Abs (cheap file read). Does not touch
// cwd or AGENTS.md: branch is volatile (the agent checks out branches itself)
// while AGENTS.md is a session snapshot — see ReloadAgents.
func (c *Context) RefreshBranch() {
	c.Branch = Branch(c.Abs)
}

// ReloadAgents re-reads AGENTS.md for c.Abs, leaving cwd/branch alone. Call at
// session boundaries — /clear, /resume, compaction — not per turn: project
// instructions sit at the head of the request prefix providers cache, so
// re-reading them every turn would invalidate the cached transcript whenever
// the file changed, including when the agent edits it itself. At a boundary
// the conversation layer is being rewritten anyway, and an unchanged file
// still cache-hits.
func (c *Context) ReloadAgents() {
	c.AgentsMD = ""
	if c.Abs == "" {
		return
	}
	if target := TrustTarget(c.Abs); isTrustedTarget(target) {
		c.AgentsMD = NearestAgents(c.Abs, target)
	}
}

// DisplayPath is abs with $HOME replaced by ~ for UI.
func DisplayPath(abs string) string {
	if abs == "" {
		return "."
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return abs
	}
	if abs == home {
		return "~"
	}
	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(abs, prefix) {
		return "~" + abs[len(home):]
	}
	return abs
}

// Branch returns the current branch name (or short SHA) for dir's git root.
func Branch(dir string) string {
	root := GitRoot(dir)
	if root == "" {
		return ""
	}
	headPath, ok := gitHeadPath(root)
	if !ok {
		return ""
	}
	return parseGitHead(headPath)
}

// NearestAgents returns the nearest non-empty AGENTS.md walking from dir up to
// ceiling (inclusive). Empty ceiling means dir only.
func NearestAgents(dir, ceiling string) string {
	abs, err := absDir(dir)
	if err != nil {
		return ""
	}
	if ceiling == "" {
		ceiling = abs
	} else if c, err := absDir(ceiling); err == nil {
		ceiling = c
	} else {
		ceiling = abs
	}

	cur := abs
	for {
		if body, ok := readAgentsFile(filepath.Join(cur, agentsFileName)); ok {
			return body
		}
		if samePath(cur, ceiling) {
			return ""
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func gitHeadPath(dir string) (string, bool) {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return filepath.Join(gitPath, "HEAD"), true
	}
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir: "
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	gitdir := line[len(prefix):]
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	return filepath.Join(gitdir, "HEAD"), true
}

func parseGitHead(headPath string) string {
	data, err := os.ReadFile(headPath)
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(data))
	const prefix = "ref: refs/heads/"
	if strings.HasPrefix(ref, prefix) {
		return ref[len(prefix):]
	}
	if len(ref) >= 7 {
		return ref[:7]
	}
	return ref
}
