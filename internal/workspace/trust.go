package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axispx/zeta/internal/paths"
)

// trustedFile is the on-disk list of directories the user has approved.
const trustedFile = "trusted.json"

type trustedStore struct {
	Paths []string `json:"paths"`
}

// TrustTarget is the directory trust applies to: git root when in a repo, else cwd.
func TrustTarget(cwd string) string {
	abs, err := absDir(cwd)
	if err != nil {
		return ""
	}
	if root := GitRoot(abs); root != "" {
		return root
	}
	return abs
}

// GitRoot returns the absolute git repository root containing dir, or "".
func GitRoot(dir string) string {
	abs, err := absDir(dir)
	if err != nil {
		return ""
	}
	for {
		if _, ok := gitHeadPath(abs); ok {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		abs = parent
	}
}

// IsTrusted reports whether cwd's trust target is on the approved list.
func IsTrusted(cwd string) bool {
	return isTrustedTarget(TrustTarget(cwd))
}

// isTrustedTarget reports whether target (already normalized) is approved.
func isTrustedTarget(target string) bool {
	if target == "" {
		return false
	}
	store, err := loadTrusted()
	if err != nil {
		return false
	}
	return storeHasPath(store, target)
}

// Trust records cwd's trust target as approved for future launches.
// dir is normalized via TrustTarget (git root when in a repo).
func Trust(dir string) error {
	target := TrustTarget(dir)
	if target == "" {
		return fmt.Errorf("resolve trust path")
	}
	store, err := loadTrusted()
	if err != nil {
		return err
	}
	if storeHasPath(store, target) {
		return nil
	}
	store.Paths = append(store.Paths, target)
	return saveTrusted(store)
}

func storeHasPath(store trustedStore, target string) bool {
	for _, p := range store.Paths {
		if samePath(p, target) {
			return true
		}
	}
	return false
}

func trustedPath() string {
	home := paths.Home()
	if home == "" {
		return ""
	}
	return filepath.Join(home, trustedFile)
}

func loadTrusted() (trustedStore, error) {
	path := trustedPath()
	if path == "" {
		return trustedStore{}, fmt.Errorf("cannot resolve trust path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return trustedStore{}, nil
		}
		return trustedStore{}, fmt.Errorf("read trust: %w", err)
	}
	var store trustedStore
	if err := json.Unmarshal(data, &store); err != nil {
		// Corrupt allowlist: treat as empty so Trust can rewrite on accept.
		return trustedStore{}, nil
	}
	return store, nil
}

func saveTrusted(store trustedStore) error {
	path := trustedPath()
	if path == "" {
		return fmt.Errorf("cannot resolve trust path")
	}
	if err := paths.EnsureHome(); err != nil {
		return fmt.Errorf("create trust dir: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trust: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write trust: %w", err)
	}
	return nil
}

func absDir(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return filepath.Clean(abs), nil
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	// Resolve symlinks so /tmp vs /private/tmp (and similar) match.
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(ra) == filepath.Clean(rb)
}
