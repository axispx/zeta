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
	// AgentsMD is the nearest AGENTS.md contents (cwd → git root, or → / if no git).
	AgentsMD string
}

// Load reads cwd, git branch, and AGENTS.md from the current process directory.
func Load() Context {
	abs, err := os.Getwd()
	if err != nil {
		abs = ""
	}
	branch, agents := inspect(abs)
	return Context{
		Abs:      abs,
		Cwd:      displayCwd(),
		Branch:   branch,
		AgentsMD: agents,
	}
}

// Label is cwd, or "cwd · branch" when in a git repo.
func (c Context) Label() string {
	if c.Branch == "" {
		return c.Cwd
	}
	return c.Cwd + " · " + c.Branch
}

func displayCwd() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "."
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return wd
	}
	if wd == home {
		return "~"
	}
	if strings.HasPrefix(wd, home+string(os.PathSeparator)) {
		return "~" + wd[len(home):]
	}
	return wd
}

// inspect walks upward once from dir: nearest AGENTS.md, branch at git root.
// Stops at the git root when in a repo; otherwise walks to the filesystem root.
func inspect(dir string) (branch, agents string) {
	if dir == "" {
		return "", ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil || abs == "" {
		return "", ""
	}
	for {
		if agents == "" {
			if body, ok := readAgentsFile(filepath.Join(abs, agentsFileName)); ok {
				agents = body
			}
		}
		if headPath, ok := gitHeadPath(abs); ok {
			return parseGitHead(headPath), agents
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", agents
		}
		abs = parent
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
