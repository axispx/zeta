package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// rgWorkspaceFlags returns shared ripgrep flags for workspace searches (grep + glob).
// Hidden files on, .git out; gitignore still applies by default.
// Fresh slice each call so callers can append without aliasing a shared backing array.
func rgWorkspaceFlags() []string {
	return []string{"--hidden", "--glob", "!.git"}
}

// runRg invokes ripgrep with the given args. Exit code 1 (no matches) returns
// empty stdout and a nil error so callers can treat "no matches" uniformly.
//
// Callers should pass paths relative to root (see rgTarget). cmd.Dir is root so
// relative paths keep rg output relative and avoid a second relativize pass.
func runRg(ctx context.Context, root string, args []string) (string, error) {
	if _, err := exec.LookPath("rg"); err != nil {
		return "", fmt.Errorf("rg (ripgrep) not found on PATH; install with: brew install ripgrep")
	}
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return "", nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// rgTarget turns an absolute path under root into a path arg relative to root
// (cmd.Dir). Empty means "search from root" — omit the path arg so rg prints
// clean relative paths (no leading "./").
func rgTarget(root, abs string) (string, error) {
	if abs == "" || abs == root {
		return "", nil
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the workspace", abs)
	}
	return rel, nil
}

// resolveSearchPath resolves an optional tool path arg to an absolute path under
// root. Empty path → root.
func resolveSearchPath(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return root, nil
	}
	return resolvePath(root, path)
}

// resolveSearchDir is resolveSearchPath plus a directory check when path is set
// (glob scopes to directories only; grep may target a single file).
func resolveSearchDir(root, path string) (string, error) {
	abs, err := resolveSearchPath(root, path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return abs, nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", path)
	}
	return abs, nil
}

// linesFromRg splits rg stdout into non-empty lines (trailing newline dropped).
func linesFromRg(out string) []string {
	if out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}
