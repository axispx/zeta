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
}

// Load reads cwd and git branch from the current process directory.
func Load() Context {
	abs, err := os.Getwd()
	if err != nil {
		abs = ""
	}
	return Context{
		Abs:    abs,
		Cwd:    displayCwd(),
		Branch: gitBranch("."),
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

// gitBranch returns the current branch for dir, or "" if not in a git repo.
// Reads .git/HEAD (no subprocess) and walks parents for worktrees.
func gitBranch(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		headPath, ok := gitHeadPath(abs)
		if ok {
			return parseGitHead(headPath)
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
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
