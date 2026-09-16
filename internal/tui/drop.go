package tui

import (
	"os"
	"strings"

	"github.com/axispx/zeta/internal/image"
)

// maxDropPaths caps one payload so a stray paste of a big listing can't flood
// the composer.
const maxDropPaths = 64

// dropPaths reports whether content is a file-drop payload: one or more
// existing local paths, optionally shell-quoted, backslash-escaped, separated
// by spaces or newlines, or as file:// URIs (DnD text/uri-list).
// Returns nil unless every token is an existing path, so ordinary text keeps
// normal paste behavior.
func dropPaths(content string) []string {
	toks := splitDropTokens(content)
	if len(toks) == 0 || len(toks) > maxDropPaths {
		return nil
	}
	paths := make([]string, 0, len(toks))
	for _, tok := range toks {
		p, ok := dropPath(tok)
		if !ok {
			return nil
		}
		paths = append(paths, p)
	}
	return paths
}

// dropPath normalizes one dropped token and requires it to exist on disk.
func dropPath(tok string) (string, bool) {
	p, ok := image.NormalizePath(tok)
	if !ok {
		return "", false
	}
	abs, err := image.Abs(p)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(abs); err != nil {
		return "", false
	}
	return p, true
}

// splitDropTokens splits a payload on unquoted whitespace, keeping quotes and
// backslash escapes in the token for the normalizer. URI-list comment lines are
// dropped. Returns nil on an unbalanced quote.
func splitDropTokens(s string) []string {
	var (
		toks    []string
		cur     strings.Builder
		quote   byte
		started bool
	)
	flush := func() {
		if !started {
			return
		}
		toks = append(toks, cur.String())
		started = false
		cur.Reset()
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' && i+1 < len(s) {
				cur.WriteByte(c)
				i++
				cur.WriteByte(s[i])
				continue
			}
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
			started = true
		case c == '\\' && i+1 < len(s):
			cur.WriteByte(c)
			i++
			cur.WriteByte(s[i])
			started = true
		case c == '#' && !started: // uri-list comment runs to end of line
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil
	}
	flush()
	return toks
}

// insertDropPaths puts dropped paths at the cursor as their own prompt token:
// separated by spaces, space before when the cursor follows text, and one
// trailing space.
func (m *Model) insertDropPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	text := strings.Join(paths, " ")
	if m.needsSpaceBeforeInsert() {
		text = " " + text
	}
	text += " "
	before := m.textarea.Value()
	m.textarea.InsertString(text)
	m.notePromptEdit(before)
	m.afterComposerChange()
}

// handleDropPaste inserts a dropped-file payload. Reports false when content is
// not a drop, so plain text falls through to the textarea.
func (m *Model) handleDropPaste(content string) bool {
	paths := dropPaths(content)
	if len(paths) == 0 {
		return false
	}
	m.insertDropPaths(paths)
	return true
}
