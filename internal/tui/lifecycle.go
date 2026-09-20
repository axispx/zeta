package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/session"
)

// Session lifecycle: replaying a session's records into the UI, appending new
// ones, and the process exit handoff to main.

func loadSession(recs []session.Record) (ui []Message, history []ai.Message) {
	ui = make([]Message, 0, len(recs))
	for _, r := range recs {
		switch r.Role {
		case session.RoleUser:
			ui = append(ui, Message{Role: RoleUser, Text: userDisplayFromSession(r.Text, r.Images)})
		case session.RoleAgent:
			if r.Text != "" {
				ui = append(ui, Message{Role: RoleAgent, Text: r.Text, framePlan: r.FramePlan})
			}
		case session.RoleTool:
			label := r.Label
			if label == "" {
				label = "tool"
			}
			uiMsg := newToolMessage(label, r.Tool)
			if r.Denied {
				uiMsg.Status = ToolDenied
			} else {
				uiMsg.Status = ToolOK
			}
			if toolHasOut(r.Tool) && uiMsg.Status == ToolOK {
				uiMsg.Out = r.Text
			}
			ui = append(ui, uiMsg)
		case session.RoleCompact:
			// Full JSONL is kept for the UI; API history is rebuilt below.
			ui = append(ui, Message{Role: RoleSystem, Text: compactDividerText})
		case session.RoleError:
			ui = append(ui, Message{Role: RoleError, Text: r.Text})
		}
	}
	return ui, compact.RebuildAPIHistory(recs)
}

// persist appends one durable record, surfacing a write failure in the transcript.
func (m *Model) persist(rec session.Record) {
	m.reportSaveErr(m.session.Persist(rec))
}

// reportSaveErr surfaces a durable-write failure as a transcript error row.
func (m *Model) reportSaveErr(err error) {
	if err == nil {
		return
	}
	m.transcript.messages = append(m.transcript.messages, Message{
		Role: RoleError,
		Text: "session save failed: " + err.Error(),
	})
}

func (m *Model) requestQuit() tea.Cmd {
	m.finishTurn()
	m.cancelCompact()
	m.cancelAuthRetry()
	return m.quit()
}

func (m *Model) quit() tea.Cmd {
	m.exit.quitting = true
	return tea.Quit
}

// PersistedSessionID returns the current session id if it has been written to disk.
func (m Model) PersistedSessionID() string {
	if m.session.Log == nil || !m.session.Log.Persisted() {
		return ""
	}
	return m.session.Log.ID
}

// UpdateRequested reports that the user ran /update, so main should apply the
// release in the CLI and relaunch zeta.
func (m Model) UpdateRequested() bool {
	return m.exit.updateOnExit
}
