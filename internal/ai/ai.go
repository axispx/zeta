package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/axispx/zeta/internal/config"
)

// Role is an OpenAI chat message role.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleTool      Role = "tool"
)

// ToolCall is one function invocation requested by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Tool is a function definition advertised to the model.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Message is one turn in an API conversation.
type Message struct {
	Role       Role
	Text       string
	ToolCalls  []ToolCall // assistant messages that request tools
	ToolCallID string     // tool result messages
}

// EventType identifies a streaming event.
type EventType int

const (
	EventDelta EventType = iota
	EventDone
	EventErr
)

// Event is one item from a streaming completion.
type Event struct {
	Type    EventType
	Text    string
	Message Message // set on EventDone: assembled assistant message (content + tool calls)
	Usage   Usage   // set on EventDone when the provider reports usage
	Err     error
}

// Usage is token counts from a completion.
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// ContextTokens is how many tokens this response occupies toward the context
// window: TotalTokens when set, otherwise PromptTokens + CompletionTokens.
func (u Usage) ContextTokens() int64 {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

// Client calls OpenAI-compatible chat completion APIs.
type Client struct {
	api    openai.Client
	model  string
	effort string // reasoning_effort, empty to omit
}

// New builds a client for the given provider and model id.
func New(p config.Provider, model string) *Client {
	effort := ""
	if md, ok := p.Models[model]; ok {
		effort = strings.TrimSpace(md.ReasoningEffort)
	}
	return &Client{
		api: openai.NewClient(
			option.WithBaseURL(strings.TrimRight(p.BaseURL, "/")),
			option.WithAPIKey(p.AuthToken()),
		),
		model:  model,
		effort: effort,
	}
}

// Stream runs a streaming chat completion. tools may be nil/empty.
// The returned channel is closed when the stream finishes (after a final Done
// or Err event, or on cancel).
func (c *Client) Stream(ctx context.Context, msgs []Message, tools []Tool) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)
		c.stream(ctx, msgs, tools, out)
	}()
	return out
}

func (c *Client) stream(ctx context.Context, msgs []Message, tools []Tool, out chan<- Event) {
	apiMsgs, err := toAPIMessages(msgs)
	if err != nil {
		out <- Event{Type: EventErr, Err: err}
		return
	}

	params := openai.ChatCompletionNewParams{
		Model:    c.model,
		Messages: apiMsgs,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}
	if c.effort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(c.effort)
	}
	if len(tools) > 0 {
		params.Tools = toAPITools(tools)
	}

	stream := c.api.Chat.Completions.NewStreaming(ctx, params)
	acc := openai.ChatCompletionAccumulator{}

	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		if len(chunk.Choices) == 0 {
			continue
		}
		if delta := chunk.Choices[0].Delta.Content; delta != "" {
			out <- Event{Type: EventDelta, Text: delta}
		}
	}

	if err := stream.Err(); err != nil {
		if ctx.Err() != nil {
			out <- Event{Type: EventDone}
			return
		}
		out <- Event{Type: EventErr, Err: err}
		return
	}

	out <- Event{Type: EventDone, Message: assistantFromAcc(acc), Usage: usageFromAcc(acc)}
}

func assistantFromAcc(acc openai.ChatCompletionAccumulator) Message {
	if len(acc.Choices) == 0 {
		return Message{Role: RoleAssistant}
	}
	m := acc.Choices[0].Message
	out := Message{
		Role: RoleAssistant,
		Text: m.Content,
	}
	for _, tc := range m.ToolCalls {
		if tc.Type != "" && tc.Type != "function" {
			continue
		}
		name := tc.Function.Name
		if name == "" {
			continue
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out
}

func usageFromAcc(acc openai.ChatCompletionAccumulator) Usage {
	return Usage{
		PromptTokens:     acc.Usage.PromptTokens,
		CompletionTokens: acc.Usage.CompletionTokens,
		TotalTokens:      acc.Usage.TotalTokens,
	}
}

func toAPITools(tools []Tool) []openai.ChatCompletionToolUnionParam {
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Name,
			Description: openai.String(t.Description),
			Parameters:  shared.FunctionParameters(params),
		}))
	}
	return out
}

// Complete runs a non-streaming chat completion and returns the assistant text.
// maxTokens caps the completion when > 0.
func (c *Client) Complete(ctx context.Context, msgs []Message, maxTokens int64) (string, error) {
	return c.complete(ctx, msgs, maxTokens)
}

func (c *Client) complete(ctx context.Context, msgs []Message, maxTokens int64) (string, error) {
	apiMsgs, err := toAPIMessages(msgs)
	if err != nil {
		return "", err
	}

	params := openai.ChatCompletionNewParams{
		Model:    c.model,
		Messages: apiMsgs,
	}
	if maxTokens > 0 {
		params.MaxCompletionTokens = openai.Int(maxTokens)
	}

	res, err := c.api.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", err
	}
	if len(res.Choices) == 0 {
		return "", fmt.Errorf("empty completion")
	}
	return res.Choices[0].Message.Content, nil
}

func toAPIMessages(msgs []Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	apiMsgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case RoleUser:
			apiMsgs = append(apiMsgs, openai.UserMessage(m.Text))
		case RoleAssistant:
			apiMsgs = append(apiMsgs, assistantParam(m))
		case RoleSystem, RoleDeveloper:
			// developer is OpenAI-specific; map to system for compatible APIs.
			apiMsgs = append(apiMsgs, openai.SystemMessage(m.Text))
		case RoleTool:
			apiMsgs = append(apiMsgs, openai.ToolMessage(m.Text, m.ToolCallID))
		default:
			return nil, fmt.Errorf("unknown message role %q", m.Role)
		}
	}
	return apiMsgs, nil
}

func assistantParam(m Message) openai.ChatCompletionMessageParamUnion {
	if len(m.ToolCalls) == 0 {
		return openai.AssistantMessage(m.Text)
	}
	var asst openai.ChatCompletionAssistantMessageParam
	if m.Text != "" {
		asst.Content.OfString = openai.String(m.Text)
	}
	for _, tc := range m.ToolCalls {
		asst.ToolCalls = append(asst.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: tc.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			},
		})
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &asst}
}
