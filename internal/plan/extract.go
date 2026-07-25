package plan

import (
	"strings"
	"unicode"
)

const (
	openTag  = "<proposed_plan>"
	closeTag = "</proposed_plan>"
	// fenceLang is the markdown fence language models emit by mistake
	// (```proposed_plan … ``` instead of <proposed_plan>…</proposed_plan>).
	fenceLang = "proposed_plan"
)

// Open reports whether text has an unclosed plan block (still streaming).
func Open(text string) bool {
	start, openN, kind := lastOpen(text)
	if start < 0 {
		return false
	}
	rest := text[start+openN:]
	return !hasClose(rest, kind)
}

// Extract returns the last complete plan body.
// Accepts <proposed_plan>…</proposed_plan> (preferred) or ```proposed_plan … ```.
// Body is trimmed; empty or unclosed bodies are not ok.
func Extract(text string) (body string, ok bool) {
	s := text
	for {
		start, openN, kind := lastOpen(s)
		if start < 0 {
			return "", false
		}
		rest := s[start+openN:]
		end, _ := findClose(rest, kind)
		if end < 0 {
			// Unclosed — try an earlier occurrence.
			s = s[:start]
			continue
		}
		body = strings.TrimSpace(rest[:end])
		if body == "" {
			s = s[:start]
			continue
		}
		return body, true
	}
}

// DisplayParts splits assistant text for transcript rendering.
// Delimiters are never included in the returned strings. ok is true when a plan
// block is present (complete, or still open at the end while streaming).
func DisplayParts(text string) (before, planBody, after string, ok bool) {
	start, openN, kind := lastOpen(text)
	if start < 0 {
		return text, "", "", false
	}
	before = strings.TrimRight(text[:start], " \t")
	before = strings.TrimSuffix(before, "\n")
	rest := text[start+openN:]
	rest = strings.TrimPrefix(rest, "\n")

	if end, closeN := findClose(rest, kind); end >= 0 {
		planBody = strings.TrimSpace(rest[:end])
		after = strings.TrimPrefix(rest[end+closeN:], "\n")
		after = strings.TrimLeft(after, " \t")
		if planBody == "" {
			// Empty closed block — fall through as if no plan.
			return text, "", "", false
		}
		return before, planBody, after, true
	}
	// Unclosed: treat remainder as live plan body (hide the open delimiter).
	planBody = strings.TrimRight(rest, " \t\n")
	if planBody == "" {
		return before, "", "", false
	}
	return before, planBody, "", true
}

// Title returns a short title from plan markdown (first heading or first line).
func Title(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "Untitled plan"
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeftFunc(line, func(r rune) bool {
			return r == '#' || unicode.IsSpace(r)
		})
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return truncateRunes(line, 80)
	}
	return "Untitled plan"
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

// BuildPrompt is the user message seeded into a fresh Build session.
func BuildPrompt(body string) string {
	body = strings.TrimSpace(body)
	return "Implement this approved plan. Treat it as the source of truth — do not re-litigate decided tradeoffs.\n\n" + body
}

// delimKind is how a plan block is delimited.
type delimKind int

const (
	delimTag delimKind = iota
	delimFence
)

// lastOpen finds the last plan open delimiter.
// Returns start index, open delimiter length (through end of open line for fences),
// and kind. Fence form: ```proposed_plan at line start.
func lastOpen(s string) (idx, openN int, kind delimKind) {
	tagI := strings.LastIndex(s, openTag)
	fenceI, fenceN := lastFenceOpen(s)
	switch {
	case tagI < 0 && fenceI < 0:
		return -1, 0, 0
	case tagI > fenceI:
		return tagI, len(openTag), delimTag
	default:
		return fenceI, fenceN, delimFence
	}
}

// lastFenceOpen returns the index and full open-line length of the last
// ```proposed_plan fence (backticks at line start; optional trailing spaces on the line).
func lastFenceOpen(s string) (idx, openN int) {
	needle := "```" + fenceLang
	last, lastN := -1, 0
	for i := 0; ; {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			return last, lastN
		}
		abs := i + j
		if fenceAtLineStart(s, abs) {
			after := abs + len(needle)
			// Language token must end: EOL, whitespace only until EOL (no suffix).
			n, ok := fenceOpenLen(s, abs, after)
			if ok {
				last, lastN = abs, n
			}
		}
		i = abs + 1
	}
}

// fenceOpenLen returns how many bytes the open fence line consumes from abs,
// including trailing spaces and a single trailing \n if present.
func fenceOpenLen(s string, abs, afterLang int) (n int, ok bool) {
	i := afterLang
	for i < len(s) {
		switch s[i] {
		case ' ', '\t':
			i++
		case '\r':
			i++
			if i < len(s) && s[i] == '\n' {
				i++
			}
			return i - abs, true
		case '\n':
			return i + 1 - abs, true
		default:
			// Non-space after language (e.g. ```proposed_plan_extra) — reject.
			return 0, false
		}
	}
	// EOF after language / spaces — open fence at end of string.
	return i - abs, true
}

func fenceAtLineStart(s string, idx int) bool {
	for i := idx - 1; i >= 0; i-- {
		switch s[i] {
		case ' ', '\t':
			continue
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

func hasClose(rest string, kind delimKind) bool {
	end, _ := findClose(rest, kind)
	return end >= 0
}

// findClose returns the body-end index and the close delimiter length.
func findClose(rest string, kind delimKind) (end, closeLen int) {
	if kind == delimTag {
		i := strings.Index(rest, closeTag)
		if i < 0 {
			return -1, 0
		}
		return i, len(closeTag)
	}
	return findFenceClose(rest)
}

// findFenceClose finds a line that is only backticks (``` or longer).
func findFenceClose(rest string) (end, closeLen int) {
	i := 0
	for i < len(rest) {
		nl := strings.IndexByte(rest[i:], '\n')
		var line string
		lineEnd := len(rest)
		if nl >= 0 {
			line = rest[i : i+nl]
			lineEnd = i + nl
		} else {
			line = rest[i:]
		}
		line = strings.TrimSuffix(line, "\r")
		if isCloseFence(strings.TrimSpace(line)) {
			// Consume the line including its trailing newline when present.
			n := lineEnd - i
			if lineEnd < len(rest) && rest[lineEnd] == '\n' {
				n++
			}
			return i, n
		}
		if nl < 0 {
			break
		}
		i = lineEnd + 1
	}
	return -1, 0
}

func isCloseFence(s string) bool {
	if len(s) < 3 {
		return false
	}
	for _, c := range s {
		if c != '`' {
			return false
		}
	}
	return true
}
