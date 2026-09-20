package tui

import (
	"encoding/json"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/tools"
)

// openInteractiveTool picks the widget for a core.WaitInteractive tool.
// Add cases here as interactive tools land; each must be flagged by
// tools.Interactive() so the agent gates it.
func (m *Model) openInteractiveTool(name string, argsJSON json.RawMessage) {
	switch name {
	case tools.AskUser:
		m.openAskFromToolStart(argsJSON)
	default:
		// Policy says interactive but no UI — let the model recover.
		m.sendReply(agent.InjectResult("error: no harness UI for tool " + name))
	}
}
