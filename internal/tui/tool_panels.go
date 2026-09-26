package tui

import (
	"encoding/json"

	"github.com/axispx/zeta/internal/harness"
	"github.com/axispx/zeta/internal/tools"
)

// openInteractiveTool picks the widget for a harness.WaitInteractive tool.
// Add cases here as interactive tools land; each must be flagged by
// tools.Interactive() so the harness gates it.
func (m *Model) openInteractiveTool(name string, argsJSON json.RawMessage) {
	switch name {
	case tools.AskUser:
		m.openAskFromToolStart(argsJSON)
	default:
		// Policy says interactive but no UI — let the model recover.
		m.sendReply(harness.InjectResult("error: no harness UI for tool " + name))
	}
}
