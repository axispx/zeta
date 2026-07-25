package permission

import "testing"

func TestSideEffect(t *testing.T) {
	for _, name := range []string{"bash", "edit", "write"} {
		if !SideEffect(name) {
			t.Errorf("%s should be side-effect", name)
		}
	}
	for _, name := range []string{"read", "grep", "glob", "websearch", "webfetch", ""} {
		if SideEffect(name) {
			t.Errorf("%s should not be side-effect", name)
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
	if !s.Granted("write") {
		t.Fatal("edit class covers write")
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
	s.Grant("edit")
	if NeedsDecision(&s, "write") {
		t.Fatal("edit class covers write")
	}
}
