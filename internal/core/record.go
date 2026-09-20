package core

import (
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/session"
)

// RecordFromAPI maps one API transcript message to a durable session record.
// The reverse direction (records → API history) is compact.RebuildAPIHistory.
func RecordFromAPI(m ai.Message) session.Record {
	switch m.Role {
	case ai.RoleUser:
		return session.Record{
			Role:   session.RoleUser,
			Text:   m.Text,
			Images: m.Images,
		}
	case ai.RoleAssistant:
		rec := session.Record{Role: session.RoleAgent, Text: m.Text}
		for _, tc := range m.ToolCalls {
			rec.ToolCalls = append(rec.ToolCalls, session.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
		return rec
	case ai.RoleTool:
		return session.Record{
			Role:       session.RoleTool,
			Text:       m.Text,
			ToolCallID: m.ToolCallID,
		}
	default:
		return session.Record{Role: session.RoleError, Text: m.Text}
	}
}

// ToolRecord is RecordFromAPI plus the tool-row fields the transcript needs.
func ToolRecord(m ai.Message, label, name string, denied bool) session.Record {
	rec := RecordFromAPI(m)
	rec.Label = label
	rec.Tool = name
	rec.Denied = denied
	return rec
}
