package tools

import (
	"fmt"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

const (
	maxDiffBytes  = 100 * 1024
	truncNoteDiff = "\n\n[truncated: showing first %d bytes]"
)

// unifiedDiff returns a unified diff for before→after, or "" if identical.
// path is used in ---/+++ headers. Large diffs are truncated like other tool outputs.
func unifiedDiff(path, before, after string) string {
	diff := strings.TrimSuffix(udiff.Unified(path, path, before, after), "\n")
	return truncateDiff(diff)
}

func truncateDiff(diff string) string {
	if len(diff) <= maxDiffBytes {
		return diff
	}
	cut := maxDiffBytes
	if i := strings.LastIndex(diff[:cut], "\n"); i > 0 {
		cut = i
	}
	return diff[:cut] + fmt.Sprintf(truncNoteDiff, cut)
}
