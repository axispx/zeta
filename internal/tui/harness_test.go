package tui

import (
	"testing"

	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/tools"
)

// isolateZetaHome points ZETA_HOME at a temp dir so tests do not touch the
// developer's real ~/.zeta (sessions, trusted.json, config).
func isolateZetaHome(t *testing.T) {
	t.Helper()
	t.Setenv("ZETA_HOME", t.TempDir())
}

func TestWaitFor(t *testing.T) {
	var grants permission.Session

	if g := waitFor(tools.AskUser, &grants); g != waitInteractive {
		t.Fatalf("ask_user: %v", g)
	}
	if g := waitFor(tools.Bash, &grants); g != waitPermission {
		t.Fatalf("bash ungated: %v", g)
	}
	if g := waitFor(tools.Edit, &grants); g != waitPermission {
		t.Fatalf("edit: %v", g)
	}
	if g := waitFor(tools.Read, &grants); g != waitNone {
		t.Fatalf("read: %v", g)
	}

	grants.Grant(tools.Bash)
	if g := waitFor(tools.Bash, &grants); g != waitNone {
		t.Fatalf("bash session-granted: %v", g)
	}
	// edit never session-grantable
	grants.Grant(tools.Edit)
	if g := waitFor(tools.Edit, &grants); g != waitPermission {
		t.Fatalf("edit still waits: %v", g)
	}
	// interactive wins even if somehow permission would also apply
	if g := waitFor(tools.AskUser, &grants); g != waitInteractive {
		t.Fatalf("interactive priority: %v", g)
	}
}

func TestBottomSlotExclusive(t *testing.T) {
	var b bottomSlot
	b.setPerm(&permissionPrompt{name: tools.Bash})
	if b.perm == nil || b.ask != nil || b.plan != nil {
		t.Fatalf("setPerm: %+v", b)
	}
	b.setAsk(newAskPrompt(sampleAskArgs()))
	if b.ask == nil || b.perm != nil || b.plan != nil {
		t.Fatalf("setAsk clears perm: %+v", b)
	}
	b.setPlan(&planPrompt{body: "x", title: "T"})
	if b.plan == nil || b.ask != nil || b.perm != nil {
		t.Fatalf("setPlan clears ask: %+v", b)
	}
	b.clear()
	if b.blocked() {
		t.Fatal("clear")
	}
}
