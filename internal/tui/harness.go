package tui

import (
	"encoding/json"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/tools"
)

// waitKind is the single harness-wait classifier for Gate and tool-start UI.
type waitKind int

const (
	waitNone waitKind = iota
	waitPermission
	waitInteractive
)

// waitFor classifies whether the harness must decide before a tool runs, and how.
// Interactive tools always wait; side-effect tools wait unless session-granted.
// Gate and handleTurnToolStart both use this — do not re-branch the policy elsewhere.
func waitFor(name string, grants *permission.Session) waitKind {
	if tools.Interactive(name) {
		return waitInteractive
	}
	if permission.NeedsDecision(grants, name) {
		return waitPermission
	}
	return waitNone
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
