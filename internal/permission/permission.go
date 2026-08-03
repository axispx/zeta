package permission

import (
	"sync"

	"github.com/axispx/zeta/internal/tools"
)

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
)

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
