package tools

import (
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

// unifiedDiff returns a unified diff for before→after, or "" if identical.
// path is used in ---/+++ headers. Edit/write results skip limitToolOutput so
// the full patch reaches the UI and model.
func unifiedDiff(path, before, after string) string {
	return strings.TrimSuffix(udiff.Unified(path, path, before, after), "\n")
}
