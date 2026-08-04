//go:build unix

package config

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// withConfigFileLock runs fn while holding an exclusive flock on path+".lock".
// Serializes OAuth refresh and config writes across zeta processes so a
// single-use refresh token cannot be redeemed twice.
func withConfigFileLock(path string, fn func() error) error {
	if path == "" {
		return fn()
	}
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("config lock: %w", err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("config lock: %w", err)
	}
	defer func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }()
	return fn()
}
