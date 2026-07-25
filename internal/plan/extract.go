package plan

import (
	"strings"
	"unicode"
)

const (
	openTag  = "<proposed_plan>"
	closeTag = "</proposed_plan>"
)

// Open reports whether text has an unclosed <proposed_plan> (still streaming).
func Open(text string) bool {
	start := strings.LastIndex(text, openTag)
	if start < 0 {
		return false
	}
	return !strings.Contains(text[start+len(openTag):], closeTag)
}

// Extract returns the last complete <proposed_plan>…</proposed_plan> body.
// Tags are case-sensitive. Body is trimmed; empty or unclosed bodies are not ok.
func Extract(text string) (body string, ok bool) {
	s := text
	for {
		start := strings.LastIndex(s, openTag)
		if start < 0 {
			return "", false
		}
		rest := s[start+len(openTag):]
		end := strings.Index(rest, closeTag)
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
// Tags are never included in the returned strings. ok is true when a plan
// block is present (complete, or still open at the end while streaming).
func DisplayParts(text string) (before, planBody, after string, ok bool) {
	start := strings.LastIndex(text, openTag)
	if start < 0 {
		return text, "", "", false
	}
	before = strings.TrimRight(text[:start], " \t")
	before = strings.TrimSuffix(before, "\n")
	rest := text[start+len(openTag):]
	rest = strings.TrimPrefix(rest, "\n")

	if end := strings.Index(rest, closeTag); end >= 0 {
		planBody = strings.TrimSpace(rest[:end])
		after = strings.TrimPrefix(rest[end+len(closeTag):], "\n")
		after = strings.TrimLeft(after, " \t")
		if planBody == "" {
			// Empty closed block — fall through as if no plan.
			return text, "", "", false
		}
		return before, planBody, after, true
	}
	// Unclosed: treat remainder as live plan body (hide the open tag).
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
