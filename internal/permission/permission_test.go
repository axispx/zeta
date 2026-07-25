package permission

import "testing"

func TestSideEffect(t *testing.T) {
	for _, name := range []string{"bash", "edit", "write"} {
		if !SideEffect(name) {
			t.Errorf("%s should be side-effect", name)
		}
	}
	for _, name := range []string{"read", "grep", "glob", "websearch", "webfetch", "skill", ""} {
		if SideEffect(name) {
			t.Errorf("%s should not be side-effect", name)
		}
	}
}

func TestClassOf(t *testing.T) {
	if c, ok := ClassOf("bash"); !ok || c != ClassBash {
		t.Fatalf("bash: %v %v", c, ok)
	}
	if c, ok := ClassOf("write"); !ok || c != ClassEdit {
		t.Fatalf("write: %v %v", c, ok)
	}
	if _, ok := ClassOf("read"); ok {
		t.Fatal("read has no class")
	}
}

func TestSessionGrantable(t *testing.T) {
	if !SessionGrantable("bash") {
		t.Fatal("bash should be session-grantable")
	}
	for _, name := range []string{"edit", "write", "read", ""} {
		if SessionGrantable(name) {
			t.Fatalf("%s must not be session-grantable", name)
		}
	}
}

func TestSessionGrant(t *testing.T) {
	var s Session
	if s.Granted("bash") {
		t.Fatal("empty session")
	}
	s.Grant("bash")
	if !s.Granted("bash") {
		t.Fatal("bash grant")
	}
	s.Grant("edit")
	if s.Granted("edit") || s.Granted("write") {
		t.Fatal("edit/write must never receive a session grant")
	}
	if s.Granted("read") {
		t.Fatal("read has no class")
	}
}

func TestNilSession(t *testing.T) {
	var s *Session
	if s.Granted("bash") {
		t.Fatal("nil")
	}
	s.Grant("bash") // must not panic
	if !NeedsDecision(nil, "bash") {
		t.Fatal("nil session: bash needs decision")
	}
	if !NeedsDecision(nil, "edit") {
		t.Fatal("nil session: edit needs decision")
	}
	if NeedsDecision(nil, "read") {
		t.Fatal("read never needs decision")
	}
}

func TestNeedsDecision(t *testing.T) {
	var s Session
	if !NeedsDecision(&s, "bash") || !NeedsDecision(&s, "write") {
		t.Fatal("ungranted side-effect")
	}
	if NeedsDecision(&s, "read") {
		t.Fatal("read")
	}
	s.Grant("bash")
	if NeedsDecision(&s, "bash") {
		t.Fatal("bash granted")
	}
	s.Grant("edit")
	if !NeedsDecision(&s, "write") || !NeedsDecision(&s, "edit") {
		t.Fatal("edit class must still need a decision after Grant")
	}
}
