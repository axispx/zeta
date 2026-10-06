package ai

import (
	"context"
	"errors"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/image"
)

// ErrAuth reports the provider rejected the credential (HTTP 401).
// The TUI refreshes OAuth tokens and retries the turn once when this surfaces.
var ErrAuth = errors.New("authentication failed")

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

// Image is a multimodal image on a user message (data: URL + MIME).
type Image = image.Ref

// Message is one turn in an API conversation.
type Message struct {
	Role       Role
	Text       string
	Images     []Image    // user messages only; data: URLs
	ToolCalls  []ToolCall // assistant messages that request tools
	ToolCallID string     // tool result messages
}

// EventType identifies a streaming event.
type EventType int

const (
	EventDelta EventType = iota
	EventDone
	EventErr
	// EventReasoning is streamed reasoning / thinking tokens (not answer content).
	// Appended after existing types so their iota values stay stable.
	EventReasoning
)

// Event is one item from a streaming completion.
type Event struct {
	Type    EventType
	Text    string
	Message Message // set on EventDone: assembled assistant message (content + tool calls)
	Usage   Usage   // set on EventDone when the provider reports usage
	// Plan is the subscription quota the provider reported, when it meters by
	// plan rather than per token (codex). Nil for API-billed providers.
	Plan *codex.PlanUsage
	Err  error
}

// Usage is token counts from a completion. Fields carry JSON tags because
// sessions persist per-turn usage verbatim (see session.Record).
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens int64 `json:"completion_tokens,omitempty"`
	TotalTokens      int64 `json:"total_tokens,omitempty"`
	// CachedTokens is the prompt prefix the provider served from its prompt
	// cache. CacheReported separates a genuine 0% hit rate from a provider
	// that does not report cache accounting at all.
	CachedTokens  int64 `json:"cached_tokens,omitempty"`
	CacheReported bool  `json:"cache_reported,omitempty"`
	// CacheWriteTokens is prompt written to the provider's cache by this
	// request. Zero on providers that cache implicitly at no extra cost.
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
}

// ContextTokens is how many tokens this response occupies toward the context
// window: TotalTokens when set, otherwise PromptTokens + CompletionTokens.
func (u Usage) ContextTokens() int64 {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

// apiClient is the transport a Client drives: OpenAI-compatible Chat
// Completions, or the Responses API of the ChatGPT Codex backend.
type apiClient interface {
	// stream runs one streaming completion, emitting deltas and a final Done
	// or Err event on out.
	stream(ctx context.Context, model, effort string, msgs []Message, tools []Tool, out chan<- Event)
	// complete runs one non-streaming completion and returns the assistant text.
	complete(ctx context.Context, model, effort string, msgs []Message, tools []Tool, maxTokens int64) (string, error)
}

// Client calls a provider's chat completion API.
type Client struct {
	api    apiClient
	model  string
	effort string // reasoning_effort, empty to omit
}

// New builds a client for the given provider and model id.
// Providers whose base URL is the Codex backend get the Responses transport;
// everything else speaks OpenAI-compatible Chat Completions.
func New(p config.Provider, model string) *Client {
	effort := ""
	if md, ok := p.Models[model]; ok {
		effort = strings.TrimSpace(md.ReasoningEffort)
	}
	var api apiClient
	if codex.IsEndpoint(p.BaseURL) {
		api = newCodexClient(p)
	} else {
		api = &chatClient{api: openai.NewClient(
			option.WithBaseURL(strings.TrimRight(p.BaseURL, "/")),
			option.WithAPIKey(p.AuthToken()),
		)}
	}
	return &Client{api: api, model: model, effort: effort}
}

// Stream runs a streaming chat completion. tools may be nil/empty.
// The returned channel is closed when the stream finishes (after a final Done
// or Err event, or on cancel).
func (c *Client) Stream(ctx context.Context, msgs []Message, tools []Tool) <-chan Event {
	out := make(chan Event, 16)
	go func() {
		defer close(out)
		c.api.stream(ctx, c.model, c.effort, msgs, tools, out)
	}()
	return out
}

// Complete runs a non-streaming chat completion and returns the assistant text.
// maxTokens caps the completion when > 0.
//
// tools are sent verbatim with tool_choice "none". Providers fold the tool
// array into the cached request prefix, so passing a conversation's own tools
// keeps that prefix intact — the caller is reusing a warm prefix, not asking
// for a tool to run — while the choice keeps the model from calling one.
func (c *Client) Complete(ctx context.Context, msgs []Message, tools []Tool, maxTokens int64) (string, error) {
	return c.api.complete(ctx, c.model, c.effort, msgs, tools, maxTokens)
}
