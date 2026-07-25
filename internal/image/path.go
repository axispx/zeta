// Package image normalizes pasted paths, sniffs local image files, builds
// data: URLs for session/API, and reads clipboard images.
package image

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// NormalizePath turns a pasted path or file:// URL into a local filesystem path.
// Returns ok=false when the paste is not a single local path candidate.
func NormalizePath(pasted string) (string, bool) {
	s := singleLinePaste(pasted)
	if s == "" {
		return "", false
	}
	s = trimMatchingQuotes(s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}

	if strings.HasPrefix(strings.ToLower(s), "file://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", false
		}
		// file:///Users/x → /Users/x; file://localhost/Users/x → /Users/x
		p := u.Path
		if p == "" {
			return "", false
		}
		if dec, err := url.PathUnescape(p); err == nil {
			p = dec
		}
		// Windows file:///C:/foo
		if len(p) >= 3 && p[0] == '/' && unicode.IsLetter(rune(p[1])) && p[2] == ':' {
			p = p[1:]
		}
		s = p
	}

	s = lightUnescape(s)
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "\n\r\x00") {
		return "", false
	}
	// Reject obvious non-paths (URLs, spaces-only multi tokens without path sep).
	if strings.Contains(s, "://") {
		return "", false
	}
	// Single token preferred; allow spaces only if it looks like a path.
	if strings.ContainsAny(s, " \t") {
		if !strings.ContainsAny(s, `/\`) && !looksLikeWindowsPath(s) {
			return "", false
		}
	}
	return s, true
}

// singleLinePaste accepts a sole non-empty line (terminals often add a trailing newline).
func singleLinePaste(pasted string) string {
	s := strings.TrimSpace(pasted)
	if s == "" {
		return ""
	}
	if !strings.ContainsAny(s, "\n\r") {
		return s
	}
	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		if ln != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) != 1 {
		return ""
	}
	return lines[0]
}

func trimMatchingQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	pairs := [][2]byte{{'"', '"'}, {'\'', '\''}, {'`', '`'}}
	for _, p := range pairs {
		if s[0] == p[0] && s[len(s)-1] == p[1] {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// lightUnescape handles a Codex/OpenCode subset of shell escapes in pasted paths.
func lightUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case ' ', '\t', '(', ')', '[', ']', '{', '}', '&', ';', '|',
				'<', '>', '*', '?', '~', '`', '"', '\'', '\\', '#', '$':
				b.WriteByte(next)
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func looksLikeWindowsPath(s string) bool {
	if len(s) >= 3 && unicode.IsLetter(rune(s[0])) && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	return strings.HasPrefix(s, `\\`)
}

// Abs resolves a normalized path to an absolute path.
func Abs(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	} else if path == "~" {
		return os.UserHomeDir()
	}
	return filepath.Abs(path)
}
