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
	m.session.WS = workspace.Context{Abs: root}
	m.session.Grants = &grants
	m.session.Rules = rules
	replies := make(chan agent.Reply, 1)
	m.turn.current = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	// The exact Gate the agent loop runs with.
	gate := core.Gate(rules, &grants, root)
	if !gate(tools.Bash, bashArgs("go test")) {
		t.Fatal("probe setup: ungated bash must block the agent")
	}

	// Mid-turn: the user presses [p] on the prompt.
	m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test", args: bashArgs("go test")})
	if m.panel.perm == nil {
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
	if m.panel.perm != nil {
		t.Fatal("harness must not open a panel for an allowed call")
	}
	select {
	case r := <-replies:
		t.Fatalf("no reply expected for a call the agent does not await: %+v", r)
	default:
	}
}

func TestPanelExclusive(t *testing.T) {
	var p panel
	p.setPerm(&permissionPrompt{name: tools.Bash})
	if p.perm == nil || p.ask != nil || p.plan != nil {
		t.Fatalf("setPerm: %+v", p)
	}
	p.setAsk(newAskPrompt(sampleAskArgs()))
	if p.ask == nil || p.perm != nil || p.plan != nil {
		t.Fatalf("setAsk clears perm: %+v", p)
	}
	p.setPlan(&planPrompt{body: "x", title: "T"})
	if p.plan == nil || p.ask != nil || p.perm != nil {
		t.Fatalf("setPlan clears ask: %+v", p)
	}
	p.clear()
	if p.blocked() {
		t.Fatal("clear")
	}
}
