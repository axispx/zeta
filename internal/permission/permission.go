package permission

import "sync"

// SideEffect reports whether a tool can mutate the workspace or run process work
// and therefore needs a human decision before running.
func SideEffect(tool string) bool {
	_, ok := ClassOf(tool)
	return ok
}

// NeedsDecision reports whether the harness must ask before this tool runs
// (side-effect tool with no session grant).
func NeedsDecision(grants *Session, tool string) bool {
	return SideEffect(tool) && !grants.Granted(tool)
}

// Class groups side-effect tools that share a session grant.
type Class int

const (
	ClassBash Class = iota
	ClassEdit
)

// ClassOf maps a side-effect tool to its grant class.
func ClassOf(tool string) (Class, bool) {
	switch tool {
	case "bash":
		return ClassBash, true
	case "edit", "write":
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
)

// Session holds "allow for session" grants (harness-owned).
type Session struct {
	mu sync.Mutex
	ok map[Class]bool
}

// Granted reports whether the tool's class was previously allowed for the session.
func (s *Session) Granted(tool string) bool {
	if s == nil {
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

// Grant allows the tool's class for the rest of the session.
func (s *Session) Grant(tool string) {
	if s == nil {
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
