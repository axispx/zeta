package tui

import (
	"encoding/json"
	"testing"

	"github.com/axispx/zeta/internal/harness"
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

// TestGateAndUIShareLiveRules is the regression guard for a mid-turn rule
// persist: the loop's gate and the UI must classify against the same rules,
// or the UI stays silent while the gate blocks forever.
func TestGateAndUIShareLiveRules(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	var grants permission.Session
	rules := permission.NewRules(policy.Policy{})

	m := testModel()
	m.session.WS = workspace.Context{Abs: root}
	m.session.Grants = &grants
	m.session.Rules = rules
	replies := make(chan harness.Reply, 1)
	m.turn.current = &turnSession{activeTool: -1, ch: make(chan harness.Event), reply: replies, cancel: func() {}}

	// The exact Gate the loop runs with.
	gate := harness.Gate(rules, &grants, root)
	if !gate(tools.Bash, bashArgs("go test")) {
		t.Fatal("probe setup: ungated bash must block the loop")
	}

	// Mid-turn: the user presses [p] on the prompt.
	m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test", args: bashArgs("go test")})
	if m.panel.perm == nil {
		t.Fatal("expected the first prompt")
	}
	m.decidePermission(permission.AllowAlways, "")
	if r := <-replies; r.Kind != harness.ReplyRun {
		t.Fatalf("allow-always should allow the current call: %+v", r)
	}

	// Next call: loop and UI must agree (both run, no reply, no panel).
	if gate(tools.Bash, bashArgs("go test -v")) {
		t.Fatal("gate should see the persisted rule")
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test -v", args: bashArgs("go test -v")})
	if m.panel.perm != nil {
		t.Fatal("UI must not open a panel for an allowed call")
	}
	select {
	case r := <-replies:
		t.Fatalf("no reply expected for a call the loop does not await: %+v", r)
	default:
	}
}

func TestPanelExclusive(t *testing.T) {
	var p panel
	p.setPerm(&permissionPrompt{name: tools.Bash})
	if p.perm == nil || p.ask != nil {
		t.Fatalf("setPerm: %+v", p)
	}
	p.setAsk(newAskPrompt(sampleAskArgs()))
	if p.ask == nil || p.perm != nil {
		t.Fatalf("setAsk clears perm: %+v", p)
	}
	p.setPerm(&permissionPrompt{name: tools.Bash})
	if p.perm == nil || p.ask != nil {
		t.Fatalf("setPerm clears ask: %+v", p)
	}
	p.clear()
	if p.blocked() {
		t.Fatal("clear")
	}
}
