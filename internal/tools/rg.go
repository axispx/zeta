package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
