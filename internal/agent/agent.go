package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/tools"
)

// EventKind identifies a turn event.
type EventKind int

const (
	// KindDelta is streamed assistant text.
	KindDelta EventKind = iota
	// KindToolStart is a tool call beginning (Text=label, Name=tool).
	// Path/Detail carry optional preview for the harness. When Gate asks
	// for a decision, the agent waits on Replies before running the tool.
	KindToolStart
	// KindToolOut is live tool output so far (Text=snapshot, Name=tool). May be dropped if the UI is behind.
	KindToolOut
	// KindTool is a finished tool call; Message is the tool result for the API transcript.
	// Denied is set when the call was rejected (permission deny or cancel).
	KindTool
	// KindAssistant is a completed assistant message (may include tool calls).
	KindAssistant
	// KindDone means the turn finished successfully.
	KindDone
	// KindErr means the turn failed.
	KindErr
	// KindReasoning is streamed reasoning / thinking tokens (UI only; not answer text).
	// Appended after existing kinds so their iota values stay stable.
	KindReasoning
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
	Path    string // KindToolStart: workspace path when relevant
	Detail  string // KindToolStart: side-effect preview (diff); empty when unused
	Denied  bool   // KindTool: rejected by harness deny or cancel
}

// Config runs a streaming completion with a tool loop.
type Config struct {
	Client   *ai.Client
	Tools    []tools.Tool
	Root     string
	MaxTurns int // <=0 means unlimited
	// Replies is harness-owned: one allow/deny per gated KindToolStart.
	// true = proceed, false = reject without running. Nil skips waiting.
	Replies <-chan bool
	// Gate reports whether the harness must decide before this tool runs.
	// Nil means never wait. Ignored when Replies is nil.
	Gate func(name string) bool
}

// Run executes completions and tools until the model stops calling tools,
// an optional MaxTurns limit is hit, or ctx is cancelled. history is the
// durable API transcript (user/assistant/tool only); system/developer must
// already be prepended by the caller.
func (c Config) Run(ctx context.Context, history []ai.Message) <-chan Event {
	out := make(chan Event, eventBuffer)
	go func() {
		defer close(out)
		c.run(ctx, history, out)
	}()
	return out
}

func (c Config) run(ctx context.Context, history []ai.Message, out chan<- Event) {
	maxTurns := c.MaxTurns // <=0: unlimited
	defs := tools.Defs(c.Tools)

	for turn := 0; ; turn++ {
		if ctx.Err() != nil {
			out <- Event{Kind: KindDone}
			return
		}
		if maxTurns > 0 && turn == maxTurns {
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
			label, result, denied := c.execTool(ctx, call, out)
			history = append(history, result)
			out <- Event{Kind: KindTool, Text: label, Name: call.Name, Message: result, Denied: denied}
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
		case ai.EventReasoning:
			if evt.Text != "" {
				out <- Event{Kind: KindReasoning, Text: evt.Text}
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

func (c Config) execTool(ctx context.Context, call ai.ToolCall, ev chan<- Event) (label string, result ai.Message, denied bool) {
	args := json.RawMessage(call.Arguments)
	label = toolLabel(c.Tools, call.Name, args)

	ev <- Event{
		Kind:   KindToolStart,
		Text:   label,
		Name:   call.Name,
		Path:   tools.ArgPath(args),
		Detail: tools.Preview(c.Tools, call.Name, c.Root, args),
	}
	if err := c.awaitReply(ctx, call.Name); err != nil {
		return label, denialResult(call, err.Error()), true
	}

	ctx = tools.WithProgress(ctx, func(s string) {
		emitToolOut(ev, call.Name, s)
	})
	var out string
	if !json.Valid(args) {
		out = "error: invalid JSON arguments"
	} else {
		out = tools.Run(ctx, c.Tools, c.Root, call.Name, args)
	}
	return label, ai.Message{
		Role:       ai.RoleTool,
		Text:       out,
		ToolCallID: call.ID,
	}, false
}

// awaitReply waits for a harness allow/deny when Replies is set and Gate asks.
// Nil Replies or a false/nil Gate skips the wait. false / cancel reject.
func (c Config) awaitReply(ctx context.Context, name string) error {
	if c.Replies == nil || c.Gate == nil || !c.Gate(name) {
		return nil
	}
	select {
	case allow := <-c.Replies:
		if !allow {
			return fmt.Errorf("the user denied this call")
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("cancelled")
	}
}

func toolLabel(ts []tools.Tool, name string, args json.RawMessage) string {
	if t, ok := tools.ByName(ts, name); ok {
		return t.Summary(args)
	}
	if t, ok := tools.ByName(tools.Build(), name); ok {
		return t.Summary(args)
	}
	return name
}

func denialResult(call ai.ToolCall, reason string) ai.Message {
	return ai.Message{
		Role:       ai.RoleTool,
		Text:       fmt.Sprintf("rejected: %s", reason),
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
