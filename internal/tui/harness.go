package tui

import (
	"encoding/json"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
)

// waitKind is the single harness-wait classifier for Gate and tool-start UI.
type waitKind int

const (
	waitNone waitKind = iota
	waitPermission
	waitInteractive
	waitAutoDeny
)

// waitFor classifies whether the harness must decide before a tool runs, and how.
// Interactive tools always wait; otherwise permission.Classify decides run / ask /
// deny (policy rules first, then session grants). Gate and handleTurnToolStart
// both use this — do not re-branch the policy elsewhere.
func waitFor(rules *permission.Rules, grants *permission.Session, root, name string, args json.RawMessage) waitKind {
	if tools.Interactive(name) {
		return waitInteractive
	}
	switch permission.Classify(rules, grants, root, name, args) {
	case policy.Deny:
		return waitAutoDeny
	case policy.Ask:
		return waitPermission
	default:
		return waitNone
	}
}

// gateFor is the agent-side Gate: it waits exactly when the harness will handle
// the tool start. It closes over the live rules/grants holders, so a mid-turn
// rule persist is visible to both sides of the decision.
func gateFor(rules *permission.Rules, grants *permission.Session, root string) func(string, json.RawMessage) bool {
	return func(name string, args json.RawMessage) bool {
		return waitFor(rules, grants, root, name, args) != waitNone
	}
}

// interactiveOpener opens harness UI for an interactive tool.
// Single registry: add openers here; tools.Interactive() flags the tool.
type interactiveOpener func(m *Model, args json.RawMessage)

// interactiveOpeners is the sole map of interactive tool name → UI opener.
// tools.Interactive(name) must be true for every key (enforced by askUserTool.Interactive).
var interactiveOpeners = map[string]interactiveOpener{
	tools.AskUser: (*Model).openAskFromToolStart,
}

// openInteractiveTool opens harness UI for waitInteractive tools via the registry.
func (m *Model) openInteractiveTool(name string, argsJSON json.RawMessage) {
	if open, ok := interactiveOpeners[name]; ok {
		open(m, argsJSON)
		return
	}
	// Policy says interactive but no UI — let the model recover.
	m.sendReply(agent.InjectResult("error: no harness UI for tool " + name))
}
