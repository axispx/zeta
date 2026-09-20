package tui

import (
	"encoding/json"
	"testing"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/core"
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

// TestGateAndHarnessShareLiveRules is the regression guard for a mid-turn rule
// persist: the agent Gate and the harness must classify against the same rules,
// or the harness stays silent while the gate blocks forever.
func TestGateAndHarnessShareLiveRules(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	var grants permission.Session
	rules := permission.NewRules(policy.Policy{})

	m := testModel()
	m.WS = workspace.Context{Abs: root}
	m.Grants = &grants
	m.Rules = rules
	replies := make(chan agent.Reply, 1)
	m.turn = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	// The exact Gate the agent loop runs with.
	gate := core.Gate(rules, &grants, root)
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
