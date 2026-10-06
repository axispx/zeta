// Package permission decides allow or deny for side-effect tool calls.
package permission

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"

	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
)

// Call is the permission-relevant view of one tool call, derived once (CallFor)
// so the classifier, the persisted rule, and the approval prompt cannot disagree.
type Call struct {
	// Match is the input to Policy.Evaluate. Command is the trimmed bash command;
	// Path is the workspace-relative edit/write/read target (empty when an
	// edit/write escapes; outside reads use the absolute path so deny globs match).
	Match policy.Match
	// Rule is the single allow rule an "always allow" decision persists — the one
	// part of the call that still needed approval, or the target file. Persist
	// reports whether there is one to write.
	Rule    policy.Rule
	Persist bool
	// Outside reports a read/edit/write target that escapes the workspace (prompt mark).
	Outside bool
	// Dir is the outside-read approval boundary (the file's parent, or the path
	// itself when it is a directory). Empty when not an outside read.
	Dir string
}

// CallFor derives the permission view of a tool call against the live policy.
// The policy is needed because the rule an "always allow" writes is the part of
// the call that is not already covered by it.
func CallFor(plan policy.Policy, root, tool string, args json.RawMessage) Call {
	c := Call{Match: policy.Match{Tool: tool}}
	switch tool {
	case tools.Bash:
		command := tools.ArgCommand(args)
		c.Match.Command = command
		// The rule an "always allow" writes: the prefix the model proposed when
		// it is sound, otherwise the first part that still needs approval.
		if rule, ok := plan.RememberRule(tools.Bash, command); ok {
			c.Rule = rule
			c.Persist = true
		}
		if prefix := tools.ArgPrefixRule(args); len(prefix) > 0 {
			if rule, ok := plan.RequestedRule(tools.Bash, prefix, command); ok {
				c.Rule = rule
				c.Persist = true
			}
		}
	case tools.Edit, tools.Write:
		rel, outside := tools.EditTarget(root, tools.ArgPath(args))
		c.Match.Path = rel
		c.Outside = outside
		if rel != "" {
			c.Rule = policy.Rule{Tool: tool, Path: rel, Action: policy.ActionAllow}
			c.Persist = true
		}
	case tools.Read:
		abs, rel, outside := tools.ResolveTarget(root, tools.ArgPath(args))
		c.Outside = outside
		if outside {
			// Absolute path so a hand-written deny like **/.ssh/** still matches.
			c.Match.Path = filepath.ToSlash(abs)
			c.Dir = tools.ExternalDir(abs)
		} else {
			c.Match.Path = rel
			if policy.EnvFile(rel) && rel != "" {
				c.Rule = policy.Rule{Tool: tools.Read, Path: rel, Action: policy.ActionAllow}
				c.Persist = true
			}
		}
	}
	return c
}

// Classify decides whether a tool call runs (policy.Allow), asks (policy.Ask), or
// is denied (policy.Deny). Order: a policy deny always wins (over a session grant
// too); then a session grant for this exact command; then an outside-read
// directory grant; then a policy allow; then tools that need no decision;
// otherwise ask.
func Classify(rules *Rules, grants *Session, root, tool string, args json.RawMessage) policy.Outcome {
	call := CallFor(rules.Policy(), root, tool, args)
	outcome := rules.Policy().Evaluate(call.Match)
	switch {
	case outcome == policy.Deny:
		return policy.Deny
	case grants.CmdGranted(call.Match.Command):
		return policy.Allow
	case grants.DirGranted(call) && !policy.EnvFile(call.Match.Path):
		return policy.Allow
	case outcome == policy.Allow:
		return policy.Allow
	case needsAsk(call):
		return policy.Ask
	default:
		return policy.Allow
	}
}

// needsAsk reports whether this call requires a human decision when no
// policy/session rule decided. bash/edit/write always do; read asks when the
// target is outside the workspace or is a dotenv secret (.env / .env.*, not
// .env.example).
func needsAsk(call Call) bool {
	if SideEffect(call.Match.Tool) {
		return true
	}
	if call.Match.Tool != tools.Read {
		return false
	}
	return call.Outside || policy.EnvFile(call.Match.Path)
}

// SideEffect reports whether a tool always needs a human decision before running
// (mutate the workspace or run process work). Outside reads ask separately via
// needsAsk; a session grant is per directory, not the read tool as a class.
func SideEffect(tool string) bool {
	_, ok := ClassOf(tool)
	return ok
}

// SessionGrantable reports whether "allow for session" is offered for this tool.
// File mutations (edit/write) always require a per-call review. Outside reads
// offer a directory-scoped session grant via GrantDir, not a command grant.
func SessionGrantable(tool string) bool {
	c, ok := ClassOf(tool)
	return ok && c != ClassEdit
}

// Class groups side-effect tools for harness UI (prompt copy) and session grants.
type Class int

// The side-effect tool classes.
const (
	ClassBash Class = iota
	ClassEdit
)

// ClassOf maps a side-effect tool to its UI/grant class.
func ClassOf(tool string) (Class, bool) {
	switch tool {
	case tools.Bash:
		return ClassBash, true
	case tools.Edit, tools.Write:
		return ClassEdit, true
	default:
		return 0, false
	}
}

// Decision is the harness reply after KindToolStart when gating is enabled.
type Decision int

// The harness's answers to a gated tool call.
const (
	AllowOnce Decision = iota
	AllowSession
	Deny
	// AllowAlways additionally persists an allow rule via policy.Add.
	AllowAlways
)

// Rules is the live permission policy, shared by the agent gate (its own
// goroutine) and the harness classifier. Replace swaps it in place so both always
// classify against the same rules — same reason Session is a shared holder.
type Rules struct {
	mu  sync.RWMutex
	pol policy.Policy
}

// NewRules returns a live rule set seeded with the startup policy.
func NewRules(p policy.Policy) *Rules { return &Rules{pol: p} }

// Policy returns the current policy. Safe on a nil receiver (empty policy).
func (r *Rules) Policy() policy.Policy {
	if r == nil {
		return policy.Policy{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pol
}

// Replace swaps the live policy (e.g. after persisting a new rule).
// Safe on a nil receiver (no-op).
func (r *Rules) Replace(p policy.Policy) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pol = p
}

// Session holds "allow for session" grants (harness-owned): the exact shell
// commands the user approved for this session, and the directories allowed for
// outside reads. A command grant is the command text as it was approved, so it
// covers the call the prompt showed and nothing else — the session row must not
// turn one approved command into a blanket shell approval.
type Session struct {
	mu   sync.Mutex
	cmds map[string]bool
	dirs []string
}

// CmdGranted reports whether this exact shell command was allowed for the session.
func (s *Session) CmdGranted(command string) bool {
	if s == nil || command == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmds[command]
}

// GrantCmd allows one exact shell command for the rest of the session. No-op for
// an empty command.
func (s *Session) GrantCmd(command string) {
	if s == nil || command == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmds == nil {
		s.cmds = map[string]bool{}
	}
	s.cmds[command] = true
}

// GrantDir allows outside reads under dir for the rest of the session.
// No-op for empty or ".".
func (s *Session) GrantDir(dir string) {
	if s == nil {
		return
	}
	dir = filepath.Clean(dir)
	if dir == "" || dir == "." {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.dirs {
		if d == dir {
			return
		}
	}
	s.dirs = append(s.dirs, dir)
}

// DirGranted reports whether an outside read's path is under a session-granted
// directory. In-workspace and non-read calls never match.
func (s *Session) DirGranted(call Call) bool {
	if s == nil || call.Match.Tool != tools.Read || !call.Outside {
		return false
	}
	abs := filepath.FromSlash(call.Match.Path)
	if abs == "" {
		return false
	}
	abs = filepath.Clean(abs)
	sep := string(filepath.Separator)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, dir := range s.dirs {
		if abs == dir || strings.HasPrefix(abs, dir+sep) {
			return true
		}
	}
	return false
}
