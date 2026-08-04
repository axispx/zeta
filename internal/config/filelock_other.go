//go:build !unix

package config

// withConfigFileLock runs fn. Non-unix builds have no flock; the process
// mutex in refreshOAuth still single-flights within one zeta.
func withConfigFileLock(path string, fn func() error) error {
	return fn()
}
