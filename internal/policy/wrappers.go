package policy

import "strings"

// stripWrappers removes the leading programs that only run the rest of the
// command — `timeout 30 go test` is `go test` — so a rule or the read-only
// shortcut judges the command that actually runs. The list is fixed: `timeout`,
// `time`, `nice`, `nohup`, `stdbuf`, plus the builtins `command`, `builtin` and
// `noglob`. Runners that execute their arguments but add their own semantics
// (`env`, `sudo`, `npx`, `docker exec`, `watch`, `find -exec`) are deliberately
// absent and keep asking.
//
// A wrapper whose options are not recognised is left alone, so an unfamiliar
// form is judged as written rather than guessed at.
func stripWrappers(part string) string {
	for {
		rest, ok := stripWrapper(part)
		if !ok {
			return part
		}
		part = rest
	}
}

// stripWrapper removes one leading wrapper and its options from part.
func stripWrapper(part string) (string, bool) {
	name, rest, ok := nextWord(part)
	if !ok {
		return "", false
	}
	switch name {
	case "nohup", "builtin", "noglob":
	case "time":
		rest = skipWords(rest, func(w string) int {
			if w == "-p" {
				return 1
			}
			return 0
		})
	case "command":
		// `command -v ls` looks a program up rather than running it.
		if w, _, ok := nextWord(rest); !ok || strings.HasPrefix(w, "-") {
			return "", false
		}
	case "nice":
		var bad bool
		rest = skipWords(rest, func(w string) int {
			switch {
			case w == "-n":
				return 2
			case strings.HasPrefix(w, "--adjustment="), isNiceLevel(w):
				return 1
			case strings.HasPrefix(w, "-"):
				bad = true
			}
			return 0
		})
		if bad {
			return "", false
		}
	case "timeout":
		var bad bool
		rest = skipWords(rest, func(w string) int {
			switch {
			case w == "-s", w == "-k", w == "--signal", w == "--kill-after":
				return 2
			case w == "-p", w == "-v", w == "--preserve-status", w == "--foreground", w == "--verbose",
				strings.HasPrefix(w, "--signal="), strings.HasPrefix(w, "--kill-after="):
				return 1
			case strings.HasPrefix(w, "-"):
				bad = true
			}
			return 0
		})
		if bad {
			return "", false
		}
		// The duration is not optional: `timeout ls` is not a wrapped `ls`.
		w, after, ok := nextWord(rest)
		if !ok || !isDuration(w) {
			return "", false
		}
		rest = after
	case "stdbuf":
		var bad bool
		rest = skipWords(rest, func(w string) int {
			switch {
			case w == "-i", w == "-o", w == "-e":
				return 2
			case len(w) > 2 && (strings.HasPrefix(w, "-i") || strings.HasPrefix(w, "-o") || strings.HasPrefix(w, "-e")),
				strings.HasPrefix(w, "--input="), strings.HasPrefix(w, "--output="), strings.HasPrefix(w, "--error="):
				return 1
			case strings.HasPrefix(w, "-"):
				bad = true
			}
			return 0
		})
		if bad {
			return "", false
		}
	default:
		return "", false
	}
	rest = strings.TrimLeft(rest, " \t")
	return rest, rest != ""
}

// skipWords drops leading option words from s. take reports how many words an
// option consumes (1 for a flag, 2 for a flag and its value) and 0 to stop.
func skipWords(s string, take func(word string) int) string {
	for {
		w, rest, ok := nextWord(s)
		if !ok {
			return s
		}
		n := take(w)
		if n == 0 {
			return s
		}
		s = rest
		if n == 2 {
			if _, after, ok := nextWord(s); ok {
				s = after
			}
		}
	}
}

// nextWord splits the first blank-separated word off s. ok is false for an
// empty s or a word with quoting, which is not a plain wrapper argument.
func nextWord(s string) (word, rest string, ok bool) {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return "", "", false
	}
	end := strings.IndexAny(s, " \t")
	if end < 0 {
		end = len(s)
	}
	word = s[:end]
	if strings.ContainsAny(word, `'"\`) {
		return "", "", false
	}
	return word, s[end:], true
}

// isNiceLevel reports whether w is `-N`, nice's short form of `-n N`.
func isNiceLevel(w string) bool {
	return len(w) > 1 && w[0] == '-' && allDigits(w[1:])
}

// isDuration reports whether w is a timeout duration: a number with an optional
// s, m, h or d suffix.
func isDuration(w string) bool {
	if n := len(w); n > 1 && strings.IndexByte("smhd", w[n-1]) >= 0 {
		w = w[:n-1]
	}
	if w == "" {
		return false
	}
	dot := false
	for i := 0; i < len(w); i++ {
		switch {
		case w[i] >= '0' && w[i] <= '9':
		case w[i] == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return w != "."
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
