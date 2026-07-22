package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/axispx/zeta/internal/config"
)

// Role is an OpenAI chat message role.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
)

// Message is one turn in an API conversation.
type Message struct {
	Role Role
	Text string
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
	Type EventType
	Text string
	Err  error
}

// Client calls OpenAI-compatible chat completion APIs.
type Client struct {
	api   openai.Client
	model string
}

// New builds a client for the given provider and model id.
func New(p config.Provider, model string) *Client {
	return &Client{
		api: openai.NewClient(
			option.WithBaseURL(strings.TrimRight(p.BaseURL, "/")),
			option.WithAPIKey(p.APIKey),
		),
		model: model,
	}
}

// Stream runs a streaming chat completion. The returned channel is closed when
// the stream finishes (after a final Done or Err event, or on cancel).
func (c *Client) Stream(ctx context.Context, msgs []Message) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)
		c.stream(ctx, msgs, out)
	}()
	return out
}

func (c *Client) stream(ctx context.Context, msgs []Message, out chan<- Event) {
	apiMsgs, err := toAPIMessages(msgs)
	if err != nil {
		out <- Event{Type: EventErr, Err: err}
		return
	}

	stream := c.api.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    c.model,
		Messages: apiMsgs,
	})

	for stream.Next() {
		evt := stream.Current()
		if len(evt.Choices) == 0 {
			continue
		}
		if delta := evt.Choices[0].Delta.Content; delta != "" {
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
	out <- Event{Type: EventDone}
}

// Complete runs a non-streaming chat completion and returns the assistant text.
func (c *Client) Complete(ctx context.Context, msgs []Message) (string, error) {
	return c.complete(ctx, msgs, 0)
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
			apiMsgs = append(apiMsgs, openai.AssistantMessage(m.Text))
		case RoleSystem, RoleDeveloper:
			// developer is OpenAI-specific; map to system for compatible APIs.
			apiMsgs = append(apiMsgs, openai.SystemMessage(m.Text))
		default:
			return nil, fmt.Errorf("unknown message role %q", m.Role)
		}
	}
	return apiMsgs, nil
}

// SessionTitle asks the model for a short chat title from the first user prompt.
func (c *Client) SessionTitle(ctx context.Context, prompt string) (string, error) {
	text, err := c.complete(ctx, []Message{
		{
			Role: RoleSystem,
			Text: "Generate a short session title (3-7 words) for this session. " +
				"Plain text only — no quotes, no punctuation at the ends, no markdown.",
		},
		{Role: RoleUser, Text: prompt},
	}, 32)
	if err != nil {
		return "", err
	}
	return cleanTitle(text), nil
}

func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"'")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	for _, p := range []string{"Title:", "title:", "Session:", "session:"} {
		s = strings.TrimSpace(strings.TrimPrefix(s, p))
	}
	s = strings.Trim(s, "`\"'")
	return strings.TrimSpace(s)
}
