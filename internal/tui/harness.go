package tui

import (
	"encoding/json"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/tools"
)

// openInteractiveTool opens harness UI for core.WaitInteractive tools.
// Single dispatch: add cases here as interactive tools land; the tool must be
// flagged by tools.Interactive() (enforced by askUserTool.Interactive).
// (A UI-supplied opener registry belongs on the long-lived core session in a
// later slice — this Model is copied by value, so it cannot hold live closures.)
func (m *Model) openInteractiveTool(name string, argsJSON json.RawMessage) {
	switch name {
	case tools.AskUser:
		m.openAskFromToolStart(argsJSON)
	default:
		// Policy says interactive but no UI — let the model recover.
		m.sendReply(agent.InjectResult("error: no harness UI for tool " + name))
	}
}
