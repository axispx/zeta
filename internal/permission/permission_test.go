package permission

import (
	"testing"

	"github.com/axispx/zeta/internal/tools"
)

func TestSideEffect(t *testing.T) {
	for _, name := range []string{tools.Bash, tools.Edit, tools.Write} {
		if !SideEffect(name) {
			t.Errorf("%s should be side-effect", name)
		}
	}
	for _, name := range []string{tools.Read, tools.Grep, tools.Glob, tools.WebSearch, tools.WebFetch, tools.Skill, ""} {
		if SideEffect(name) {
			t.Errorf("%s should not be side-effect", name)
		}
	}
}

func TestClassOf(t *testing.T) {
	if c, ok := ClassOf(tools.Bash); !ok || c != ClassBash {
		t.Fatalf("bash: %v %v", c, ok)
	}
	if c, ok := ClassOf(tools.Write); !ok || c != ClassEdit {
		t.Fatalf("write: %v %v", c, ok)
	}
	if _, ok := ClassOf(tools.Read); ok {
		t.Fatal("read has no class")
	}
}

func TestSessionGrantable(t *testing.T) {
	if !SessionGrantable(tools.Bash) {
		t.Fatal("bash should be session-grantable")
	}
	for _, name := range []string{tools.Edit, tools.Write, tools.Read, ""} {
		if SessionGrantable(name) {
			t.Fatalf("%s must not be session-grantable", name)
		}
	}
}

func TestSessionGrant(t *testing.T) {
	var s Session
	if s.Granted(tools.Bash) {
		t.Fatal("empty session")
	}
	s.Grant(tools.Bash)
	if !s.Granted(tools.Bash) {
		t.Fatal("bash grant")
	}
	s.Grant(tools.Edit)
	if s.Granted(tools.Edit) || s.Granted(tools.Write) {
		t.Fatal("edit/write must never receive a session grant")
	}
	if s.Granted(tools.Read) {
		t.Fatal("read has no class")
	}
}

func TestNilSession(t *testing.T) {
	var s *Session
	if s.Granted(tools.Bash) {
		t.Fatal("nil")
	}
	s.Grant(tools.Bash) // must not panic
	if !NeedsDecision(nil, tools.Bash) {
		t.Fatal("nil session: bash needs decision")
	}
	if !NeedsDecision(nil, tools.Edit) {
		t.Fatal("nil session: edit needs decision")
	}
	if NeedsDecision(nil, tools.Read) {
		t.Fatal("read never needs decision")
	}
}

func TestNeedsDecision(t *testing.T) {
	var s Session
	if !NeedsDecision(&s, tools.Bash) || !NeedsDecision(&s, tools.Write) {
		t.Fatal("ungranted side-effect")
	}
	if NeedsDecision(&s, tools.Read) {
		t.Fatal("read")
	}
	s.Grant(tools.Bash)
	if NeedsDecision(&s, tools.Bash) {
		t.Fatal("bash granted")
	}
	s.Grant(tools.Edit)
	if !NeedsDecision(&s, tools.Write) || !NeedsDecision(&s, tools.Edit) {
		t.Fatal("edit class must still need a decision after Grant")
	}
}
