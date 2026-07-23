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
	// KindToolStart is a tool call beginning (Text=label, Name=tool); UI row only.
	KindToolStart
	// KindToolOut is live tool output so far (Text=snapshot, Name=tool). May be dropped if the UI is behind.
	KindToolOut
	// KindTool is a finished tool call; Message is the tool result for the API transcript.
	KindTool
	// KindAssistant is a completed assistant message (may include tool calls).
	KindAssistant
	// KindDone means the turn finished successfully.
	KindDone
	// KindErr means the turn failed.
	KindErr
)

// eventBuffer absorbs bursts of KindToolOut without stalling tool I/O.
const eventBuffer = 32

// Event is one item from a tool-using completion turn.
type Event struct {
	Kind    EventKind
	Text    string     // delta text, tool UI label, or KindToolOut snapshot
	Name    string     // tool name for KindToolStart / KindToolOut / KindTool
	Message ai.Message // set for KindAssistant / KindTool
	Usage   ai.Usage   // set for KindAssistant when the provider reports usage
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
	out := make(chan Event, eventBuffer)
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

		asst, usage, ok := c.streamOnce(ctx, history, defs, out)
		if !ok {
			return
		}
		history = append(history, asst)
		out <- Event{Kind: KindAssistant, Message: asst, Usage: usage}

		if len(asst.ToolCalls) == 0 {
			out <- Event{Kind: KindDone}
			return
		}

		for _, call := range asst.ToolCalls {
			if ctx.Err() != nil {
				out <- Event{Kind: KindDone}
				return
			}
			label, result := c.execTool(ctx, call, out)
			history = append(history, result)
			out <- Event{Kind: KindTool, Text: label, Name: call.Name, Message: result}
		}
	}
}

func (c Config) streamOnce(ctx context.Context, history []ai.Message, defs []ai.Tool, out chan<- Event) (ai.Message, ai.Usage, bool) {
	ch := c.Client.Stream(ctx, history, defs)
	var asst ai.Message
	var usage ai.Usage
	for evt := range ch {
		switch evt.Type {
		case ai.EventDelta:
			if evt.Text != "" {
				out <- Event{Kind: KindDelta, Text: evt.Text}
			}
		case ai.EventDone:
			asst = evt.Message
			usage = evt.Usage
			if asst.Role == "" {
				asst.Role = ai.RoleAssistant
			}
		case ai.EventErr:
			if ctx.Err() != nil {
				out <- Event{Kind: KindDone}
				return ai.Message{}, ai.Usage{}, false
			}
			out <- Event{Kind: KindErr, Err: evt.Err}
			return ai.Message{}, ai.Usage{}, false
		}
	}
	// Cancelled with empty accumulator.
	if asst.Role == "" && asst.Text == "" && len(asst.ToolCalls) == 0 {
		out <- Event{Kind: KindDone}
		return ai.Message{}, ai.Usage{}, false
	}
	if asst.Role == "" {
		asst.Role = ai.RoleAssistant
	}
	return asst, usage, true
}

func (c Config) execTool(ctx context.Context, call ai.ToolCall, ev chan<- Event) (label string, result ai.Message) {
	args := json.RawMessage(call.Arguments)
	var out string
	if t, ok := tools.ByName(c.Tools, call.Name); ok {
		label = t.Summary(args)
	} else if t, ok := tools.ByName(tools.Build(), call.Name); ok {
		label = t.Summary(args)
	} else {
		label = call.Name
	}
	ev <- Event{Kind: KindToolStart, Text: label, Name: call.Name}
	ctx = tools.WithProgress(ctx, func(s string) {
		emitToolOut(ev, call.Name, s)
	})
	if !json.Valid(args) {
		out = "error: invalid JSON arguments"
	} else {
		out = tools.Run(ctx, c.Tools, c.Root, call.Name, args)
	}
	return label, ai.Message{
		Role:       ai.RoleTool,
		Text:       out,
		ToolCallID: call.ID,
	}
}

// emitToolOut sends live output without blocking the tool when the UI is behind.
// KindTool always carries the final snapshot.
func emitToolOut(ev chan<- Event, name, s string) {
	evt := Event{Kind: KindToolOut, Text: s, Name: name}
	select {
	case ev <- evt:
	default:
	}
}

type toolLimitError struct{}

func (toolLimitError) Error() string { return "tool loop limit reached" }

var errToolLimit error = toolLimitError{}
