package ai

import (
	"context"
	"strings"
	"unicode"
)

// SessionTitle asks the model for a short chat title from the first user prompt.
func (c *Client) SessionTitle(ctx context.Context, prompt string) (string, error) {
	text, err := c.complete(ctx, []Message{
		{
			Role: RoleSystem,
			Text: titleSystemPrompt,
		},
		{Role: RoleUser, Text: "Conversation to title:\n" + prompt},
	}, 32)
	if err != nil {
		return "", err
	}
	return cleanTitle(text), nil
}

// titleSystemPrompt steers a short session-list label (inspired by OpenCode's
// title agent, but written for zeta).
const titleSystemPrompt = `You name chats for a session picker. Output ONLY the title — one line, nothing else.

Rules:
- ≤50 characters
- Prefer 3–6 words
- Sentence case (capitalize the first word and proper nouns; leave the rest lowercase)
- Same language as the user
- Focus on what they'd search for later (topic / goal), not tone
- No markdown, quotes, trailing punctuation, or leading labels like "Title:"
- Don't answer the user's question
- Don't mention tools, modes, or that you're generating a title
- Skip filler words when they aren't needed (the, a, an, this, my)

Examples:
is the ask-mode prompt any good? → Ask mode prompt
fix flaky auth tests please → Flaky auth tests
why does grep truncate results → Grep truncation
add dark mode to settings → Dark mode settings
@internal/ai/ai.go streaming bug → AI streaming bug`

const maxTitleChars = 50

func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	// Prefer the first non-empty line (models sometimes add a blank lead-in).
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		s = line
		break
	}
	for _, p := range []string{"Title:", "title:", "Session:", "session:", "#", "##"} {
		s = strings.TrimSpace(strings.TrimPrefix(s, p))
	}
	s = strings.Trim(s, "`\"'#")
	s = strings.ReplaceAll(s, "&", " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, " .!?:;,—–-")
	if len(s) > maxTitleChars {
		s = strings.TrimRight(s[:maxTitleChars], " ")
		if i := strings.LastIndexByte(s, ' '); i > maxTitleChars/2 {
			s = s[:i]
		}
	}
	return sentenceCaseTitle(s)
}

// sentenceCaseTitle uppercases the first letter if the whole title is lowercase.
// Leaves mixed/Title Case alone so model output is preserved when intentional.
func sentenceCaseTitle(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	hasUpper := false
	for _, r := range runes {
		if unicode.IsUpper(r) {
			hasUpper = true
			break
		}
	}
	if hasUpper {
		return s
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
