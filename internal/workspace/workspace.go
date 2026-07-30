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
	// or cwd only when not in a repo). Empty when the folder is not trusted.
	AgentsMD string
}

// Load reads cwd, git branch, and AGENTS.md from the current process directory.
// AGENTS.md is loaded only when the trust target is approved; branch/cwd still fill.
func Load() Context {
	abs, err := os.Getwd()
	if err != nil {
		abs = ""
	}
	agents := ""
	if abs != "" {
		if target := TrustTarget(abs); isTrustedTarget(target) {
			agents = NearestAgents(abs, target)
		}
	}
	return Context{
		Abs:      abs,
		Cwd:      DisplayPath(abs),
		Branch:   Branch(abs),
		AgentsMD: agents,
	}
}

// RefreshBranch re-reads git HEAD for c.Abs. Does not touch cwd or AGENTS.md —
// those reload only at turn boundaries (system prompt / tool root).
func (c *Context) RefreshBranch() {
	c.Branch = Branch(c.Abs)
}

// Label is cwd, or "cwd · branch" when in a git repo.
func (c Context) Label() string {
	if c.Branch == "" {
		return c.Cwd
	}
	return c.Cwd + " · " + c.Branch
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
