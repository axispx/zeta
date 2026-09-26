package policy

import (
	"fmt"
	"slices"
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

// opaqueMeta are the shell characters segments cannot reason about textually:
// redirection, command substitution, subshells, and brace groups. `$` and
// backticks expand even inside double quotes, so they count there too. The
// separators (`;`, `&`, `|`, newlines) have their own cases.
//
// Redirection is opaque on purpose, even `2>/dev/null`: a redirect is where a
// command can name a path the prompt never showed, so a call that contains one
// is matched and prompted whole rather than split.
const opaqueMeta = "<>$`(){}"

// segments splits command on its top-level separators — `;`, `&&`, `||`, `|`,
// `|&`, `&`, and newlines — returning the trimmed sub-commands a shell would
// run, in order. ok is false when command is not a plain sequence: empty, a
// dangling piece (`a ;; b`), unbalanced quotes, or redirection / substitution /
// subshell syntax. Callers then match the string as one unit.
func segments(command string) ([]string, bool) {
	var (
		segs []string
		cur  strings.Builder
		// dangling is true while the command is still waiting for the piece an
		// operator promised: `ls && ` is not a command yet.
		dangling bool
	)
	// put appends literal text — quoted, escaped, or an ordinary run of bytes.
	put := func(s string) {
		cur.WriteString(s)
		if strings.TrimSpace(s) != "" {
			dangling = false
		}
	}
	// flush closes the current sub-command, reporting false for an empty piece.
	flush := func() bool {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		dangling = false
		if s == "" {
			return false
		}
		segs = append(segs, s)
		return true
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch c {
		case '\'':
			// Single quotes are literal: no escapes, no expansion, no separators.
			j := strings.IndexByte(command[i+1:], '\'')
			if j < 0 {
				return nil, false
			}
			put(command[i : i+j+2])
			i += j + 1
		case '"':
			end, ok := doubleQuoted(command, i)
			if !ok {
				return nil, false
			}
			put(command[i:end])
			i = end - 1
		case '\\':
			if i+1 >= len(command) {
				return nil, false
			}
			put(command[i : i+2])
			i++
		case ';', '\n', '\r':
			if !flush() {
				return nil, false
			}
		case '&', '|':
			if !flush() {
				return nil, false
			}
			dangling = true
			// Consume the doubled operator (`&&`, `||`, `|&`) so it splits once.
			if i+1 < len(command) && (command[i+1] == c || (c == '|' && command[i+1] == '&')) {
				i++
			}
		default:
			if strings.IndexByte(opaqueMeta, c) >= 0 {
				return nil, false
			}
			put(string(c))
		}
	}
	// A trailing separator ends the last command only for `;` and newline: a
	// dangling `&&` leaves the shell waiting for the rest of the command.
	if strings.TrimSpace(cur.String()) != "" {
		flush()
	}
	if dangling || len(segs) == 0 {
		return nil, false
	}
	return segs, true
}

// doubleQuoted returns the index just past the closing quote of the
// double-quoted string starting at start. ok is false when it is unterminated
// or contains expansion syntax, which a prefix match cannot see through.
func doubleQuoted(s string, start int) (int, bool) {
	for i := start + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // the escaped byte is literal, including a quote
		case '"':
			return i + 1, true
		case '$', '`':
			return 0, false
		}
	}
	return 0, false
}

// SimpleCommand reports whether command is one shell command free of chaining,
// piping, redirection, substitution, and subshell syntax. Only a simple command
// may be matched by a prefix rule: a string prefix cannot prove that
// `go test && rm -rf /` is the approved `go test`. Quoted metacharacters are
// literal, so `git commit -m "fix; tests"` is simple.
func SimpleCommand(command string) bool {
	segs, ok := segments(command)
	return ok && len(segs) == 1
}

// CommandPrefixMatch reports whether command starts with prefix at a word
// boundary, and command is simple. This implements "commands that start with"
// rules without a shell parser.
func CommandPrefixMatch(prefix, command string) bool {
	if prefix == "" || command == "" || !SimpleCommand(command) {
		return false
	}
	return command == prefix || strings.HasPrefix(command, prefix+" ")
}

// partOutcome is the verdict for one sub-command: the rules first, then the
// read-only shortcut. Evaluate and RememberRule both go through it so the
// decision and the rule offered for it cannot disagree.
func (p Policy) partOutcome(tool, part string) Outcome {
	if out := p.evaluateOne(Match{Tool: tool, Command: part}); out != Ask {
		return out
	}
	if SafeCommand(part) {
		return Allow
	}
	return Ask
}

// RememberRule returns the rule a persisted allow writes for command: the first
// sub-command that still needs a decision, remembered whole. At most one rule is
// ever written for a call, and a part the policy already allows — a rule, or a
// command that only reads — is skipped, so approving never re-states a grant
// that is already in place.
//
// ok is false when there is nothing to remember: the command is not a plain
// sequence, every part already runs, or the part that needs approval is not one
// line (a newline inside a quoted argument). Callers then offer no remember row
// rather than a rule the prompt cannot show.
func (p Policy) RememberRule(tool, command string) (Rule, bool) {
	parts, ok := segments(strings.TrimSpace(command))
	if !ok {
		return Rule{}, false
	}
	for _, part := range parts {
		if p.partOutcome(tool, part) == Allow {
			continue
		}
		if strings.ContainsAny(part, "\n\r") {
			return Rule{}, false
		}
		return Rule{Tool: tool, CommandPrefix: part, Action: ActionAllow}, true
	}
	return Rule{}, false
}

// RequestedRule returns the rule to offer for a prefix the model proposed, or ok
// false to fall back to RememberRule. A proposed prefix is accepted only when it
// is a sound narrowing of this call:
//
//   - not banned — no prefix may stand for a shell or interpreter, where
//     "commands that start with `bash`" would be every command;
//   - no existing rule already matches a part, since a second, broader rule over
//     the same ground could only conflict with it;
//   - it covers the whole call. Adding it must leave every part allowed, so the
//     row can never offer a rule that would not actually stop the prompt.
func (p Policy) RequestedRule(tool string, prefix []string, command string) (Rule, bool) {
	if len(prefix) == 0 || bannedPrefix(prefix) {
		return Rule{}, false
	}
	for _, part := range commandParts(command) {
		for _, r := range p.Rules {
			if r.matches(Match{Tool: tool, Command: part}) {
				return Rule{}, false
			}
		}
	}
	rule := Rule{Tool: tool, CommandPrefix: strings.Join(prefix, " "), Action: ActionAllow}
	candidate := Policy{Rules: append(slices.Clone(p.Rules), rule)}
	if candidate.Evaluate(Match{Tool: tool, Command: command}) != Allow {
		return Rule{}, false
	}
	return rule, true
}

// commandParts returns the sub-commands of command, or the whole string when it
// cannot be split — the same units Evaluate judges.
func commandParts(command string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	if parts, ok := segments(command); ok {
		return parts
	}
	return []string{command}
}

// bannedPrefixes are leading words that must never become a standing allow on
// their own. Each one is a shell, an interpreter, or a runner that executes what
// follows, so a rule for it would approve everything.
var bannedPrefixes = [][]string{
	{"/bin/bash"}, {"/bin/bash", "-c"}, {"/bin/bash", "-lc"},
	{"/bin/sh"}, {"/bin/sh", "-c"}, {"/bin/sh", "-lc"},
	{"/bin/zsh"}, {"/bin/zsh", "-c"}, {"/bin/zsh", "-lc"},
	{"bash"}, {"bash", "-c"}, {"bash", "-lc"},
	{"sh"}, {"sh", "-c"}, {"sh", "-lc"},
	{"zsh"}, {"zsh", "-c"}, {"zsh", "-lc"},
	{"dash"}, {"dash", "-c"}, {"fish"}, {"fish", "-c"},
	{"ksh"}, {"ksh", "-c"}, {"cmd"}, {"cmd", "/c"}, {"cmd", "/k"},
	{"cmd.exe"}, {"cmd.exe", "/c"}, {"cmd.exe", "/k"},
	{"powershell"}, {"powershell", "-c"}, {"powershell", "-Command"},
	{"powershell", "-EncodedCommand"}, {"powershell", "-File"},
	{"powershell.exe"}, {"powershell.exe", "-c"}, {"powershell.exe", "-Command"},
	{"powershell.exe", "-EncodedCommand"}, {"powershell.exe", "-File"},
	{"pwsh"}, {"pwsh", "-c"}, {"pwsh", "-e"}, {"pwsh", "-ec"}, {"pwsh", "-f"},
	{"pwsh", "-Command"}, {"pwsh", "-EncodedCommand"}, {"pwsh", "-File"},
	{"env"}, {"sudo"},
	{"git"},
	{"node"}, {"node", "-e"}, {"nodejs"}, {"nodejs", "-e"},
	{"bun"}, {"bun", "-e"}, {"bun", "run"},
	{"deno"}, {"deno", "eval"},
	{"npm", "run"}, {"pnpm", "run"}, {"yarn", "run"},
	{"python"}, {"python", "-"}, {"python", "-c"},
	{"python3"}, {"python3", "-"}, {"python3", "-c"},
	{"pythonw"}, {"pyw"}, {"py"}, {"py", "-3"}, {"pypy"}, {"pypy3"},
	{"perl"}, {"perl", "-e"}, {"ruby"}, {"ruby", "-e"}, {"php"}, {"php", "-r"},
	{"lua"}, {"lua", "-e"}, {"julia"}, {"julia", "-e"},
	{"Rscript"}, {"osascript"}, {"rm"},
}

// bannedPrefix reports whether prefix is exactly one of the banned shapes. A
// longer prefix is allowed: `rm -rf build` names an operand, `ssh host` a
// target, so only the bare forms are refused.
func bannedPrefix(prefix []string) bool {
	return slices.ContainsFunc(bannedPrefixes, func(b []string) bool {
		return slices.Equal(b, prefix)
	})
}

// Policy is the persisted ordered rule list.
type Policy struct {
	Rules []Rule `json:"rules"`
}

// Match is the policy input for one tool call. Command is empty when unknown.
// Path is empty when unknown, or when an edit/write target is outside the
// workspace (those never match a path rule). Outside reads use the absolute path.
type Match struct {
	Tool    string
	Command string
	Path    string
}

// Evaluate returns Deny when any rule denies, else Allow when every part of the
// call is allowed, else Ask. Deny wins regardless of rule order. A rule with no
// Command/Path is tool-level and matches every call of the tool.
//
// A chained bash command is judged one sub-command at a time: a rule has to
// cover each part, so a `go test` allow never carries `go test && rm -rf /` and
// a chain of already-allowed commands runs without a prompt. A part covered by
// no rule still runs when it only reads (SafeCommand). A command whose parts
// cannot be split (redirect, substitution, subshell) is matched whole, with no
// part-by-part reading and no read-only shortcut.
func (p Policy) Evaluate(m Match) Outcome {
	parts, ok := segments(m.Command)
	if !ok {
		return p.evaluateOne(m)
	}
	out := Allow
	for _, part := range parts {
		switch p.partOutcome(m.Tool, part) {
		case Deny:
			return Deny
		case Ask:
			out = Ask
		}
	}
	return out
}

// evaluateOne applies the rules to one command string.
func (p Policy) evaluateOne(m Match) Outcome {
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
