package policy

import "strings"

// Glob reports whether name matches pattern:
//   - '*' matches any run of characters except '/'
//   - '**' matches any run of characters including '/'
//   - '?' matches one character except '/'
//
// A pattern with no wildcard is an exact match, so a remembered command only
// approves that exact string. Backtracking is naive (no memoization); fine for
// short, user-authored rules, not a general-purpose matcher.
func Glob(pattern, name string) bool {
	for len(pattern) > 0 {
		switch c := pattern[0]; c {
		case '*':
			if strings.HasPrefix(pattern, "**") {
				pattern = pattern[len(pattern)-len(strings.TrimLeft(pattern, "*")):]
				if pattern == "" {
					return true
				}
				for i := 0; i <= len(name); i++ {
					if Glob(pattern, name[i:]) {
						return true
					}
				}
				return false
			}
			pattern = pattern[1:]
			for i := 0; i <= len(name); i++ {
				if i > 0 && name[i-1] == '/' {
					break
				}
				if Glob(pattern, name[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(name) == 0 || name[0] == '/' {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
		default:
			if len(name) == 0 || name[0] != c {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
		}
	}
	return name == ""
}
