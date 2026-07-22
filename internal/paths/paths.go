package paths

import (
	"os"
	"path/filepath"
)

// Home returns the zeta data directory: $ZETA_HOME, or ~/.zeta.
func Home() string {
	if env := os.Getenv("ZETA_HOME"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".zeta")
}

// EnsureHome creates the zeta home directory if needed.
func EnsureHome() error {
	dir := Home()
	if dir == "" {
		return os.ErrNotExist
	}
	return os.MkdirAll(dir, 0o700)
}
