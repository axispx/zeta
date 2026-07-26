package tools

import (
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

// unifiedDiff returns a unified diff for before→after, or "" if identical.
// path is used in ---/+++ headers. Large diffs are limited like other tool outputs
// (line cap, head+tail preview, spill) via limitToolOutput in tools.Run.
func unifiedDiff(path, before, after string) string {
	return strings.TrimSuffix(udiff.Unified(path, path, before, after), "\n")
}
