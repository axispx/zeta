package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/axispx/zeta/internal/paths"
)

const (
	// Model-facing preview budget (after per-line caps).
	// Footer text is appended after this budget and may push the final string slightly over.
	maxToolBytes = 50 * 1024
	maxToolLines = 2000

	// maxLineBytes caps any single line so minified dumps cannot dominate context.
	maxLineBytes  = 2000
	maxLineSuffix = "... (line truncated)"

	// Room reserved for the middle-omit marker inside maxToolBytes.
	middleMarkerReserve = 80

	// spillMaxAge drops older cache files when writing a new spill.
	spillMaxAge = 24 * time.Hour
)

// limitToolOutput is the model-facing size/line policy for tool results
// (everything except edit/write diffs, which stay full for human review):
//  1. per-line byte cap
//  2. if still over size/line budget: spill line-capped text, keep head+tail preview
//
// Per-tool capture bounds (bash buffer, read loop, grep/glob caps) stay silent.
func limitToolOutput(s string) string {
	if s == "" {
		return s
	}
	s = truncateLongLines(s)
	nlines := countLines(s)
	if len(s) <= maxToolBytes && nlines <= maxToolLines {
		return s
	}

	fullBytes, fullLines := len(s), nlines
	path := spillToolOutput(s)
	preview := middleTruncate(s, maxToolBytes, maxToolLines)

	var b strings.Builder
	b.WriteString(preview)
	b.WriteString("\n\n")
	if path != "" {
		fmt.Fprintf(&b, "[truncated: kept head+tail; line-capped output (%d bytes, %d lines) saved to %s]\n",
			fullBytes, fullLines, path)
		b.WriteString("Inspect with bash (rg/sed/cat); the read tool is limited to the workspace.")
	} else {
		fmt.Fprintf(&b, "[truncated: kept head+tail; line-capped output was %d bytes / %d lines]",
			fullBytes, fullLines)
	}
	return b.String()
}

// cutLine shortens one line to maxLineBytes (UTF-8 safe) with a suffix marker.
func cutLine(line string) string {
	if len(line) <= maxLineBytes {
		return line
	}
	cut := maxLineBytes
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	if cut == 0 {
		return maxLineSuffix
	}
	return line[:cut] + maxLineSuffix
}

// truncateLongLines applies cutLine to every line of s.
func truncateLongLines(s string) string {
	if s == "" {
		return s
	}
	if !strings.Contains(s, "\n") {
		return cutLine(s)
	}
	var b strings.Builder
	b.Grow(min(len(s), maxToolBytes*2))
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' {
			continue
		}
		b.WriteString(cutLine(s[start:i]))
		b.WriteByte('\n')
		start = i + 1
	}
	if start < len(s) {
		b.WriteString(cutLine(s[start:]))
	}
	return b.String()
}

// middleTruncate keeps the head and tail of s within maxBytes and maxLines.
func middleTruncate(s string, maxBytes, maxLines int) string {
	if maxBytes <= 0 {
		return ""
	}
	if maxLines > 0 {
		lines := strings.Split(s, "\n")
		if len(lines) > maxLines {
			headN := maxLines / 2
			tailN := maxLines - headN
			removed := len(lines) - maxLines
			marker := fmt.Sprintf("\n... %d lines omitted ...\n", removed)
			s = strings.Join(lines[:headN], "\n") + marker + strings.Join(lines[len(lines)-tailN:], "\n")
		}
	}
	if len(s) <= maxBytes {
		return s
	}

	// Fixed-width marker so head/tail sizes are known before slicing.
	// "%d" for omitted bytes fits in 20 digits for any practical size.
	const markerTmpl = "\n... %d bytes omitted ...\n"
	const markerMaxLen = len("\n... ") + 20 + len(" bytes omitted ...\n")

	reserve := markerMaxLen
	if reserve > maxBytes/3 {
		reserve = maxBytes / 3
	}
	if maxBytes < 24 || reserve < 8 {
		return cutUTF8Prefix(s, maxBytes)
	}
	keep := maxBytes - reserve
	leftN := keep / 2
	rightN := keep - leftN
	left := cutUTF8Prefix(s, leftN)
	right := cutUTF8Suffix(s, rightN)
	if len(left)+len(right) >= len(s) {
		return cutUTF8Prefix(s, maxBytes)
	}
	omitted := len(s) - len(left) - len(right)
	marker := fmt.Sprintf(markerTmpl, omitted)
	// Marker shorter than reserve is fine; if somehow longer, trim sides once.
	if over := len(left) + len(marker) + len(right) - maxBytes; over > 0 {
		trimLeft := over / 2
		trimRight := over - trimLeft
		left = cutUTF8Prefix(left, max(0, len(left)-trimLeft))
		right = cutUTF8Suffix(right, max(0, len(right)-trimRight))
		omitted = len(s) - len(left) - len(right)
		marker = fmt.Sprintf(markerTmpl, omitted)
	}
	return left + marker + right
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func cutUTF8Prefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func cutUTF8Suffix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// spillToolOutput writes s under $ZETA_HOME/cache/tool-output (or ~/.zeta/...).
// Returns the absolute path, or "" on failure. Prunes spills older than spillMaxAge.
func spillToolOutput(s string) string {
	home := paths.Home()
	if home == "" {
		return ""
	}
	dir := filepath.Join(home, "cache", "tool-output")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	pruneToolOutputCache(dir)

	f, err := os.CreateTemp(dir, "*.txt")
	if err != nil {
		return ""
	}
	path := f.Name()
	if _, err := f.WriteString(s); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return ""
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return ""
	}
	if err := os.Chmod(path, 0o600); err != nil {
		// Non-fatal: file is still usable.
		_ = err
	}
	return path
}

func pruneToolOutputCache(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-spillMaxAge)
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
