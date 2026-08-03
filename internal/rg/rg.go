// Package rg wraps ripgrep invocation for workspace searches.
package rg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// MaxList is a hard cap on enumerated paths so huge trees stay usable.
const MaxList = 50_000

// WorkspaceFlags returns shared flags for workspace file searches.
// Hidden files on, .git out; gitignore still applies by default.
// Fresh slice each call so callers can append without aliasing a shared backing array.
func WorkspaceFlags() []string {
	return []string{"--hidden", "--glob", "!.git"}
}

// Run invokes ripgrep with the given args. Exit code 1 (no matches) returns
// empty stdout and a nil error so callers can treat "no matches" uniformly.
//
// cmd.Dir is root so relative path args keep rg output relative.
func Run(ctx context.Context, root string, args []string) (string, error) {
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
		// Canceled / killed mid-run: surface ctx error when present.
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// Lines splits rg stdout into lines (trailing newline dropped).
func Lines(out string) []string {
	if out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// SlashPaths normalizes path separators for stable slash-form output.
func SlashPaths(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = filepath.ToSlash(line)
	}
	return out
}

// ListFiles returns slash-normalized relative paths under root, honoring
// gitignore (rg --files --hidden, !.git). limit caps the result; <=0 uses MaxList.
func ListFiles(ctx context.Context, root string, limit int) ([]string, error) {
	if root == "" {
		return nil, fmt.Errorf("root is required")
	}
	if limit <= 0 {
		limit = MaxList
	}
	args := append([]string{"--files", "--sort=path"}, WorkspaceFlags()...)
	stdout, err := Run(ctx, root, args)
	if err != nil {
		return nil, err
	}
	lines := Lines(stdout)
	if len(lines) > limit {
		lines = lines[:limit]
	}
	out := SlashPaths(lines)
	// Non-nil empty slice so callers can tell "empty tree" from "no result yet".
	if out == nil {
		out = []string{}
	}
	return out, nil
}
