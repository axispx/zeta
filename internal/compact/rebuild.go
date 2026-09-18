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
			hist = append(hist, ai.Message{
				Role:   ai.RoleUser,
				Text:   r.Text,
				Images: r.Images,
			})
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

// TrimIncomplete makes the API transcript well-formed: every assistant
// tool_calls message must be answered by one tool result per call. An
// unanswered round is dropped, not truncated around — a turn cancelled after
// the assistant asked for tools leaves such a round in the middle of the
// transcript once the user moves on, and the answers either side stay valid.
func TrimIncomplete(h []ai.Message) []ai.Message {
	out := make([]ai.Message, 0, len(h))
	pending := 0  // unanswered calls in the open round
	roundAt := -1 // index in out of the assistant message that opened it
	drop := func() {
		if pending > 0 {
			out = out[:roundAt]
		}
		pending, roundAt = 0, -1
	}
	for _, m := range h {
		switch m.Role {
		case ai.RoleAssistant:
			drop()
			if n := len(m.ToolCalls); n > 0 {
				pending, roundAt = n, len(out)
			}
			out = append(out, m)
		case ai.RoleTool:
			if pending == 0 {
				continue // result with no open round: nothing it can answer
			}
			pending--
			if pending == 0 {
				roundAt = -1
			}
			out = append(out, m)
		default:
			drop()
			out = append(out, m)
		}
	}
	drop()
	return out
}
