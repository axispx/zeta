package permission

import (
	"encoding/json"
	"sync"

	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
)

// Call is the permission-relevant view of one tool call, derived once (CallFor)
// so the classifier, the persisted rule, and the approval prompt cannot disagree.
type Call struct {
	// Match is the input to Policy.Evaluate. Command is the trimmed bash command;
	// Path is the workspace-relative edit/write target, empty when it escapes.
	Match policy.Match
	// Rule is the allow rule an "always allow" decision persists; Persist reports
	// whether it is usable (a simple bash command, an in-workspace edit/write file).
	Rule    policy.Rule
	Persist bool
	// Outside reports an edit/write target that escapes the workspace (prompt mark).
	Outside bool
}

// CallFor derives the permission view of a tool call.
func CallFor(root, tool string, args json.RawMessage) Call {
	c := Call{Match: policy.Match{Tool: tool}}
	switch tool {
	case tools.Bash:
		command := tools.ArgCommand(args)
		c.Match.Command = command
		if prefix, ok := policy.DeriveCommandPrefix(command); ok {
			c.Rule = policy.Rule{Tool: tools.Bash, CommandPrefix: prefix, Action: policy.ActionAllow}
			c.Persist = true
		}
	case tools.Edit, tools.Write:
		rel, outside := tools.EditTarget(root, tools.ArgPath(args))
		c.Match.Path = rel
		c.Outside = outside
		if rel != "" {
			c.Rule = policy.Rule{Tool: tool, Path: rel, Action: policy.ActionAllow}
			c.Persist = true
		}
	}
	return c
}

// Classify decides whether a tool call runs (policy.Allow), asks (policy.Ask), or
// is denied (policy.Deny). Order: a policy deny always wins (over a session grant
// too); then a session grant; then a policy allow; then tools with no side effect;
// otherwise ask.
func Classify(rules *Rules, grants *Session, root, tool string, args json.RawMessage) policy.Outcome {
	outcome := policy.Ask
	if pol := rules.Policy(); len(pol.Rules) > 0 {
		outcome = pol.Evaluate(CallFor(root, tool, args).Match)
	}
	switch {
	case outcome == policy.Deny:
		return policy.Deny
	case grants.Granted(tool):
		return policy.Allow
	case outcome == policy.Allow:
		return policy.Allow
	case !SideEffect(tool):
		return policy.Allow
	default:
		return policy.Ask
	}
}

// SideEffect reports whether a tool can mutate the workspace or run process work
// and therefore needs a human decision before running.
func SideEffect(tool string) bool {
	_, ok := ClassOf(tool)
	return ok
}

// SessionGrantable reports whether "allow for session" is offered for this tool.
// File mutations (edit/write) always require a per-call review.
func SessionGrantable(tool string) bool {
	c, ok := ClassOf(tool)
	return ok && c != ClassEdit
}

// Class groups side-effect tools for harness UI (prompt copy) and session grants.
type Class int

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

// Session holds "allow for session" grants (harness-owned).
// Only SessionGrantable tools can be stored; edit/write are never granted.
type Session struct {
	mu sync.Mutex
	ok map[Class]bool
}

// Granted reports whether the tool's class was previously allowed for the session.
func (s *Session) Granted(tool string) bool {
	if s == nil || !SessionGrantable(tool) {
		return false
	}
	c, ok := ClassOf(tool)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ok[c]
}

// Grant allows the tool's class for the rest of the session when SessionGrantable.
// No-op for edit/write and unknown tools.
func (s *Session) Grant(tool string) {
	if s == nil || !SessionGrantable(tool) {
		return
	}
	c, ok := ClassOf(tool)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ok == nil {
		s.ok = map[Class]bool{}
	}
	s.ok[c] = true
}
