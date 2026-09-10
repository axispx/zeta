package tui

import (
	"encoding/json"
	"testing"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

// isolateZetaHome points ZETA_HOME at a temp dir so tests do not touch the
// developer's real ~/.zeta (sessions, trusted.json, config).
func isolateZetaHome(t *testing.T) {
	t.Helper()
	t.Setenv("ZETA_HOME", t.TempDir())
}

func bashArgs(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func TestWaitFor(t *testing.T) {
	var grants permission.Session
	root := t.TempDir()
	empty := permission.NewRules(policy.Policy{})

	if g := waitFor(empty, &grants, root, tools.AskUser, nil); g != waitInteractive {
		t.Fatalf("ask_user: %v", g)
	}
	if g := waitFor(empty, &grants, root, tools.Bash, bashArgs("go test")); g != waitPermission {
		t.Fatalf("bash ungated: %v", g)
	}
	if g := waitFor(empty, &grants, root, tools.Edit, json.RawMessage(`{"path":"a.go"}`)); g != waitPermission {
		t.Fatalf("edit: %v", g)
	}
	if g := waitFor(empty, &grants, root, tools.Read, json.RawMessage(`{"path":"a.go"}`)); g != waitNone {
		t.Fatalf("read: %v", g)
	}

	grants.Grant(tools.Bash)
	if g := waitFor(empty, &grants, root, tools.Bash, bashArgs("go test")); g != waitNone {
		t.Fatalf("bash session-granted: %v", g)
	}
	// edit never session-grantable
	grants.Grant(tools.Edit)
	if g := waitFor(empty, &grants, root, tools.Edit, json.RawMessage(`{"path":"a.go"}`)); g != waitPermission {
		t.Fatalf("edit still waits: %v", g)
	}
	// interactive wins even if somehow permission would also apply
	if g := waitFor(empty, &grants, root, tools.AskUser, nil); g != waitInteractive {
		t.Fatalf("interactive priority: %v", g)
	}
}

func TestWaitForPolicy(t *testing.T) {
	var grants permission.Session
	root := t.TempDir()
	args := bashArgs("go test")

	allow := permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Bash, Command: "go test", Action: policy.ActionAllow}}})
	if g := waitFor(allow, &grants, root, tools.Bash, args); g != waitNone {
		t.Fatalf("allow rule should run: %v", g)
	}

	deny := permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Bash, Command: "go test", Action: policy.ActionDeny}}})
	if g := waitFor(deny, &grants, root, tools.Bash, args); g != waitAutoDeny {
		t.Fatalf("deny rule should auto-deny: %v", g)
	}

	// Deny beats a session grant.
	grants.Grant(tools.Bash)
	if g := waitFor(deny, &grants, root, tools.Bash, args); g != waitAutoDeny {
		t.Fatalf("deny must beat grant: %v", g)
	}
}

// TestGateAndHarnessShareLiveRules is the regression guard for a mid-turn rule
// persist: the agent Gate and the harness must classify against the same rules,
// or the harness stays silent while the gate blocks forever.
func TestGateAndHarnessShareLiveRules(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	var grants permission.Session
	rules := permission.NewRules(policy.Policy{})

	m := testModel()
	m.ws = workspace.Context{Abs: root}
	m.grants = &grants
	m.rules = rules
	replies := make(chan agent.Reply, 1)
	m.turn = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	// The exact Gate the agent loop runs with.
	gate := gateFor(rules, &grants, root)
	if !gate(tools.Bash, bashArgs("go test")) {
		t.Fatal("probe setup: ungated bash must block the agent")
	}

	// Mid-turn: the user presses [p] on the prompt.
	m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test", args: bashArgs("go test")})
	if m.bottom.perm == nil {
		t.Fatal("expected the first prompt")
	}
	m.decidePermission(permission.AllowAlways)
	if r := <-replies; r.Kind != agent.ReplyRun {
		t.Fatalf("allow-always should allow the current call: %+v", r)
	}

	// Next call: agent and harness must agree (both run, no reply, no panel).
	if gate(tools.Bash, bashArgs("go test -v")) {
		t.Fatal("gate should see the persisted rule")
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test -v", args: bashArgs("go test -v")})
	if m.bottom.perm != nil {
		t.Fatal("harness must not open a panel for an allowed call")
	}
	select {
	case r := <-replies:
		t.Fatalf("no reply expected for a call the agent does not await: %+v", r)
	default:
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
