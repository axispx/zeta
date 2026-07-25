package tools

import (
	"os"
	"path/filepath"
)

// fileChange is a planned workspace mutation (shared by edit/write Run and Preview).
type fileChange struct {
	abs, rel, before, after string
}

func applyFileChange(c fileChange) error {
	if err := os.MkdirAll(filepath.Dir(c.abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.abs, []byte(c.after), 0o644)
}

// diffPreview returns a unified diff for a planned change, or fallback on error.
func diffPreview(chg fileChange, err error, fallback string) string {
	if err != nil {
		return fallback
	}
	return unifiedDiff(chg.rel, chg.before, chg.after)
}
