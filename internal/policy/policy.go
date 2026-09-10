package policy

import (
	"fmt"
	"strings"
)

// Outcome is the policy verdict for a tool call.
type Outcome int

const (
	// Ask means no rule decided the call; the harness prompts.
	Ask Outcome = iota
	// Allow means a matching allow rule approved the call.
	Allow
	// Deny means a matching deny rule rejected the call.
	Deny
)

// Rule actions.
const (
	ActionAllow = "allow"
	ActionDeny  = "deny"
)

// Rule is one permission rule. Tool is a tool name or "*"; Command globs a bash
// command, CommandPrefix matches a bash command by leading words, Path globs a
// workspace-relative edit/write path. At most one of the three is set.
type Rule struct {
	Tool          string `json:"tool"`
	Command       string `json:"command,omitempty"`        // bash: glob vs the command string
	CommandPrefix string `json:"command_prefix,omitempty"` // bash: "commands that start with"
	Path          string `json:"path,omitempty"`           // edit/write: glob vs workspace-relative path
	Action        string `json:"action"`
}

// shellMeta are the shell characters that make a command non-trivial
// (chaining, piping, redirection, substitution, subshells, newlines). A prefix
// rule never matches a command containing any of them.
const shellMeta = ";|&<>$`\n\r(){}"

// SimpleCommand reports whether command is free of shell chaining/substitution
// syntax. Only simple commands may be matched by a prefix rule: a string prefix
// cannot prove that `go test && rm -rf /` is the approved `go test`.
func SimpleCommand(command string) bool {
	return !strings.ContainsAny(command, shellMeta)
}

// CommandPrefixMatch reports whether command starts with prefix at a word
// boundary, and command is simple. It mirrors codex's "commands that start with"
// rules without a shell parser.
func CommandPrefixMatch(prefix, command string) bool {
	if prefix == "" || command == "" || !SimpleCommand(command) {
		return false
	}
	return command == prefix || strings.HasPrefix(command, prefix+" ")
}

// DeriveCommandPrefix returns the prefix a remembered bash allow/deny rule should
// use: the program, plus its first non-flag word (e.g. "go test", "git commit").
// When the second word is a flag (e.g. "rm -rf /") the whole command is kept so
// the rule does not broaden to every `rm`. ok is false for empty or non-simple
// commands, which are not rememberable.
func DeriveCommandPrefix(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" || !SimpleCommand(command) {
		return "", false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", false
	}
	if len(fields) == 1 || strings.HasPrefix(fields[1], "-") {
		return strings.Join(fields, " "), true
	}
	return fields[0] + " " + fields[1], true
}

// Policy is the persisted ordered rule list.
type Policy struct {
	Rules []Rule `json:"rules"`
}

// Match is the policy input for one tool call. Command/Path are empty when
// unknown or when the target is outside the workspace.
type Match struct {
	Tool    string
	Command string
	Path    string
}

// Evaluate returns Deny when any rule denies, else Allow when any rule allows,
// else Ask. Deny wins regardless of rule order. A rule with no Command/Path is
// tool-level and matches every call of the tool.
func (p Policy) Evaluate(m Match) Outcome {
	allowed := false
	for _, r := range p.Rules {
		if !r.matches(m) {
			continue
		}
		if r.Action == ActionDeny {
			return Deny
		}
		allowed = true
	}
	if allowed {
		return Allow
	}
	return Ask
}

// matches reports whether the rule covers the call. A Command/CommandPrefix/Path
// rule only matches when that field is known on the call.
func (r Rule) matches(m Match) bool {
	if r.Tool != "*" && r.Tool != m.Tool {
		return false
	}
	if r.Command != "" {
		if m.Command == "" || !Glob(r.Command, m.Command) {
			return false
		}
	}
	if r.CommandPrefix != "" {
		if m.Command == "" || !CommandPrefixMatch(r.CommandPrefix, m.Command) {
			return false
		}
	}
	if r.Path != "" {
		if m.Path == "" || !Glob(r.Path, m.Path) {
			return false
		}
	}
	return true
}

// Validate reports the first invalid rule.
func Validate(rules []Rule) error {
	for i, r := range rules {
		if strings.TrimSpace(r.Tool) == "" {
			return fmt.Errorf("rule %d: tool is required", i)
		}
		if r.Action != ActionAllow && r.Action != ActionDeny {
			return fmt.Errorf("rule %d: action must be %q or %q", i, ActionAllow, ActionDeny)
		}
		if n := setFields(r); n > 1 {
			return fmt.Errorf("rule %d: at most one of command, command_prefix, or path", i)
		}
	}
	return nil
}

func setFields(r Rule) int {
	n := 0
	for _, f := range []string{r.Command, r.CommandPrefix, r.Path} {
		if f != "" {
			n++
		}
	}
	return n
}
