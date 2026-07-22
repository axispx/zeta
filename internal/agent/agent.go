package agent

import (
	"context"
	"encoding/json"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/tools"
)

const MaxToolTurns = 20

// EventKind identifies a turn event.
type EventKind int

const (
	// KindDelta is streamed assistant text.
	KindDelta EventKind = iota
	// KindTool is a UI label for a tool call; Message is the tool result for the API transcript.
	KindTool
	// KindAssistant is a completed assistant message (may include tool calls).
	KindAssistant
	// KindDone means the turn finished successfully.
	KindDone
	// KindErr means the turn failed.
	KindErr
)

// Event is one item from a tool-using completion turn.
type Event struct {
	Kind    EventKind
	Text    string     // delta text or tool UI label
	Message ai.Message // set for KindAssistant / KindTool
	Err     error
}

// Config runs a streaming completion with a tool loop.
type Config struct {
	Client   *ai.Client
	Tools    []tools.Tool
	Root     string
	MaxTurns int
}

// Run executes completions and tools until the model stops calling tools,
// the turn limit is hit, or ctx is cancelled. history is the durable API
// transcript (user/assistant/tool only); system/developer must already be
// prepended by the caller.
func (c Config) Run(ctx context.Context, history []ai.Message) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)
		c.run(ctx, history, out)
	}()
	return out
}

func (c Config) run(ctx context.Context, history []ai.Message, out chan<- Event) {
	maxTurns := c.MaxTurns
	if maxTurns <= 0 {
		maxTurns = MaxToolTurns
	}
	defs := tools.Defs(c.Tools)

	for turn := 0; turn <= maxTurns; turn++ {
		if ctx.Err() != nil {
			out <- Event{Kind: KindDone}
			return
		}
		if turn == maxTurns {
			out <- Event{Kind: KindErr, Err: errToolLimit}
			return
		}

		asst, ok := c.streamOnce(ctx, history, defs, out)
		if !ok {
			return
		}
		history = append(history, asst)
		out <- Event{Kind: KindAssistant, Message: asst}

		if len(asst.ToolCalls) == 0 {
			out <- Event{Kind: KindDone}
			return
		}

		for _, call := range asst.ToolCalls {
			if ctx.Err() != nil {
				out <- Event{Kind: KindDone}
				return
			}
			label, result := c.execTool(ctx, call)
			history = append(history, result)
			out <- Event{Kind: KindTool, Text: label, Message: result}
		}
	}
}

func (c Config) streamOnce(ctx context.Context, history []ai.Message, defs []ai.Tool, out chan<- Event) (ai.Message, bool) {
	ch := c.Client.Stream(ctx, history, defs)
	var asst ai.Message
	for evt := range ch {
		switch evt.Type {
		case ai.EventDelta:
			if evt.Text != "" {
				out <- Event{Kind: KindDelta, Text: evt.Text}
			}
		case ai.EventDone:
			asst = evt.Message
			if asst.Role == "" {
				asst.Role = ai.RoleAssistant
			}
		case ai.EventErr:
			if ctx.Err() != nil {
				out <- Event{Kind: KindDone}
				return ai.Message{}, false
			}
			out <- Event{Kind: KindErr, Err: evt.Err}
			return ai.Message{}, false
		}
	}
	// Cancelled with empty accumulator.
	if asst.Role == "" && asst.Text == "" && len(asst.ToolCalls) == 0 {
		out <- Event{Kind: KindDone}
		return ai.Message{}, false
	}
	if asst.Role == "" {
		asst.Role = ai.RoleAssistant
	}
	return asst, true
}

func (c Config) execTool(ctx context.Context, call ai.ToolCall) (label string, result ai.Message) {
	args := json.RawMessage(call.Arguments)
	var out string
	if !json.Valid(args) {
		out = "error: invalid JSON arguments"
		label = call.Name
	} else {
		if t, ok := tools.ByName(c.Tools, call.Name); ok {
			label = t.Summary(args)
		} else if t, ok := tools.ByName(tools.All(), call.Name); ok {
			label = t.Summary(args)
		} else {
			label = call.Name
		}
		out = tools.Run(ctx, c.Tools, c.Root, call.Name, args)
	}
	return label, ai.Message{
		Role:       ai.RoleTool,
		Text:       out,
		ToolCallID: call.ID,
	}
}

type toolLimitError struct{}

func (toolLimitError) Error() string { return "tool loop limit reached" }

var errToolLimit error = toolLimitError{}
