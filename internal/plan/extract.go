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
	_, open := lastSpan(text)
	return open
}

// Extract returns the last complete plan body.
// Accepts <proposed_plan>…</proposed_plan> (preferred) or ```proposed_plan … ```.
// Body is trimmed; empty or unclosed bodies are not ok.
//
// Spans pair first-open → first-close of the same kind (left to right). A body
// that mentions <proposed_plan> (e.g. in an example) does not start a nested
// span and cannot steal the real closer. When several complete blocks exist,
// the last non-empty body wins.
func Extract(text string) (body string, ok bool) {
	body, open := lastSpan(text)
	if open || body == "" {
		return "", false
	}
	return body, true
}

// DisplayParts splits assistant text for transcript rendering.
// Delimiters are never included in the returned strings. ok is true when a plan
// block is present (complete, or still open at the end while streaming).
func DisplayParts(text string) (before, planBody, after string, ok bool) {
	sp, ok := lastDisplaySpan(text)
	if !ok {
		return text, "", "", false
	}
	before = strings.TrimRight(text[:sp.openAt], " \t")
	before = strings.TrimSuffix(before, "\n")

	bodyStart := sp.openAt + sp.openN
	if bodyStart < len(text) && text[bodyStart] == '\n' {
		bodyStart++
	} else if bodyStart+1 < len(text) && text[bodyStart] == '\r' && text[bodyStart+1] == '\n' {
		bodyStart += 2
	}

	if sp.closed {
		planBody = strings.TrimSpace(text[bodyStart:sp.closeAt])
		after = text[sp.closeAt+sp.closeN:]
		after = strings.TrimPrefix(after, "\n")
		after = strings.TrimLeft(after, " \t")
		if planBody == "" {
			return text, "", "", false
		}
		return before, planBody, after, true
	}
	planBody = strings.TrimRight(text[bodyStart:], " \t\n")
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

// span is one open→close (or open→EOF) plan region.
type span struct {
	openAt  int
	openN   int
	closeAt int
	closeN  int
	kind    delimKind
	closed  bool
	body    string // trimmed; set when closed, or raw remainder when open
}

// lastSpan walks left-to-right complete spans. Returns the last non-empty
// complete body, or the trailing unclosed body with open=true.
func lastSpan(text string) (body string, open bool) {
	var lastBody string
	from := 0
	for {
		sp, ok := nextSpan(text, from)
		if !ok {
			break
		}
		if !sp.closed {
			return strings.TrimSpace(sp.body), true
		}
		if sp.body != "" {
			lastBody = sp.body
		}
		from = sp.closeAt + sp.closeN
	}
	return lastBody, false
}

// lastDisplaySpan prefers a trailing unclosed span (live stream); else the last
// complete span (empty bodies skipped for ok=false via caller).
func lastDisplaySpan(text string) (span, bool) {
	var lastClosed span
	have := false
	from := 0
	for {
		sp, ok := nextSpan(text, from)
		if !ok {
			break
		}
		if !sp.closed {
			return sp, true
		}
		lastClosed = sp
		have = true
		from = sp.closeAt + sp.closeN
	}
	if !have {
		return span{}, false
	}
	return lastClosed, true
}

// nextSpan finds the earliest open at or after from and pairs it with the first
// same-kind close. Inner occurrences of the open delimiter are body text.
func nextSpan(text string, from int) (span, bool) {
	if from < 0 {
		from = 0
	}
	if from > len(text) {
		return span{}, false
	}
	rel := text[from:]
	tagI := strings.Index(rel, openTag)
	fenceI, fenceN := firstFenceOpen(rel)
	var openAt, openN int
	var kind delimKind
	switch {
	case tagI < 0 && fenceI < 0:
		return span{}, false
	case tagI >= 0 && (fenceI < 0 || tagI < fenceI):
		openAt = from + tagI
		openN = len(openTag)
		kind = delimTag
	default:
		openAt = from + fenceI
		openN = fenceN
		kind = delimFence
	}
	bodyStart := openAt + openN
	rest := text[bodyStart:]
	end, closeN := findClose(rest, kind)
	if end < 0 {
		return span{
			openAt: openAt,
			openN:  openN,
			kind:   kind,
			closed: false,
			body:   rest,
		}, true
	}
	return span{
		openAt:  openAt,
		openN:   openN,
		closeAt: bodyStart + end,
		closeN:  closeN,
		kind:    kind,
		closed:  true,
		body:    strings.TrimSpace(rest[:end]),
	}, true
}

// firstFenceOpen returns the index and full open-line length of the first
// ```proposed_plan fence in s (backticks at line start; optional trailing spaces).
func firstFenceOpen(s string) (idx, openN int) {
	needle := "```" + fenceLang
	for i := 0; ; {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			return -1, 0
		}
		abs := i + j
		if fenceAtLineStart(s, abs) {
			after := abs + len(needle)
			n, ok := fenceOpenLen(s, abs, after)
			if ok {
				return abs, n
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
