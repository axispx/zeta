package compact

import (
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/session"
)

// RebuildAPIHistory reconstructs the model-facing transcript from session records.
// Compact events replace the running history with checkpoint + retained tail, applied in order.
func RebuildAPIHistory(log []session.Record) []ai.Message {
	var hist []ai.Message
	for _, r := range log {
		switch r.Role {
		case session.RoleUser:
			hist = append(hist, ai.Message{Role: ai.RoleUser, Text: r.Text})
		case session.RoleAgent:
			asst := ai.Message{Role: ai.RoleAssistant, Text: r.Text}
			for _, tc := range r.ToolCalls {
				asst.ToolCalls = append(asst.ToolCalls, ai.ToolCall{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				})
			}
			if asst.Text != "" || len(asst.ToolCalls) > 0 {
				hist = append(hist, asst)
			}
		case session.RoleTool:
			hist = append(hist, ai.Message{
				Role:       ai.RoleTool,
				Text:       r.Text,
				ToolCallID: r.ToolCallID,
			})
		case session.RoleCompact:
			tail := retainedTail(hist, r.Tail)
			hist = append([]ai.Message{CheckpointMessage(r.Text)}, tail...)
		}
	}
	return TrimIncomplete(hist)
}

// retainedTail returns the last tailCount messages of before (clamped).
// TailCount 0 means no retained messages — only the checkpoint remains.
func retainedTail(before []ai.Message, tailCount int) []ai.Message {
	if tailCount <= 0 {
		return nil
	}
	if tailCount > len(before) {
		tailCount = len(before)
	}
	return append([]ai.Message(nil), before[len(before)-tailCount:]...)
}

// TrimIncomplete drops a trailing partial tool round so the API transcript
// never ends with assistant tool_calls lacking results (e.g. cancelled mid-turn).
func TrimIncomplete(h []ai.Message) []ai.Message {
	pending := 0
	roundStart := -1
	for i, m := range h {
		switch m.Role {
		case ai.RoleUser:
			pending = 0
			roundStart = -1
		case ai.RoleAssistant:
			if pending > 0 {
				return h[:roundStart]
			}
			if n := len(m.ToolCalls); n > 0 {
				pending = n
				roundStart = i
			}
		case ai.RoleTool:
			if pending == 0 {
				if roundStart >= 0 {
					return h[:roundStart]
				}
				return h[:i]
			}
			pending--
			if pending == 0 {
				roundStart = -1
			}
		}
	}
	if pending > 0 && roundStart >= 0 {
		return h[:roundStart]
	}
	return h
}
