package workspace

import (
	"os"
	"strings"
	"unicode/utf8"
)

const (
	agentsFileName = "AGENTS.md"
	// maxAgentsMD caps injected project instructions to keep the system prompt bounded.
	maxAgentsMD = 64 << 10
)

func readAgentsFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	truncated := false
	if len(data) > maxAgentsMD {
		data = data[:maxAgentsMD]
		truncated = true
	}
	body := strings.TrimSpace(trimIncompleteUTF8(string(data)))
	if body == "" {
		return "", false
	}
	if truncated {
		body += "\n\n[truncated]"
	}
	return body, true
}

// trimIncompleteUTF8 peels trailing bytes left by a mid-rune byte cut.
// Does not attempt to "repair" invalid UTF-8 elsewhere in the string.
func trimIncompleteUTF8(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size != 1 {
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}
