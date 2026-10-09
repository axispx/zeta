package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// chatClient speaks OpenAI-compatible Chat Completions, which is every provider
// zeta supports except the ChatGPT Codex backend.
type chatClient struct {
	api openai.Client
}

func (c *chatClient) stream(ctx context.Context, model, effort string, msgs []Message, tools []Tool, out chan<- Event) {
	apiMsgs, err := toAPIMessages(msgs)
	if err != nil {
		out <- Event{Type: EventErr, Err: err}
		return
	}

	params := openai.ChatCompletionNewParams{
		Model:    model,
		Messages: apiMsgs,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}
	if effort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(effort)
	}
	if len(tools) > 0 {
		params.Tools = toAPITools(tools)
	}

	stream := c.api.Chat.Completions.NewStreaming(ctx, params)
	acc := openai.ChatCompletionAccumulator{}
	// Usage arrives on its own chunk (StreamOptions.IncludeUsage). Keep the raw
	// JSON: the accumulator's typed usage drops provider-specific cache fields.
	var rawUsage string

	for stream.Next() {
		chunk := stream.Current()
		if r := chunk.Usage.RawJSON(); r != "" {
			rawUsage = r
		}
		acc.AddChunk(chunk)
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		// Reasoning fields are omitted from SDK types; read from raw JSON.
		if r := reasoningFromRaw(delta.RawJSON()); r != "" {
			out <- Event{Type: EventReasoning, Text: r}
		}
		if text := delta.Content; text != "" {
			out <- Event{Type: EventDelta, Text: text}
		}
	}

	if err := stream.Err(); err != nil {
		if ctx.Err() != nil {
			out <- Event{Type: EventDone}
			return
		}
		out <- Event{Type: EventErr, Err: classifyErr(err)}
		return
	}

	out <- Event{Type: EventDone, Message: assistantFromAcc(acc), Usage: usageFromAcc(acc, rawUsage)}
}

// classifyErr maps provider auth rejections to ErrAuth so callers can refresh
// credentials and retry. The provider error is wrapped for display; other
// errors pass through unchanged.
func classifyErr(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: %v", ErrAuth, err)
	}
	return err
}

// reasoningFromRaw pulls OpenAI-compatible reasoning text from a stream delta's
// raw JSON. Providers put it in reasoning_content (DeepSeek) or reasoning (others).
// SDK delta types omit these fields, so we read RawJSON on the hot path.
func reasoningFromRaw(raw string) string {
	// Most chunks are content-only; avoid json.Unmarshal when the key cannot appear.
	if raw == "" || !strings.Contains(raw, "reasoning") {
		return ""
	}
	var probe struct {
		ReasoningContent string `json:"reasoning_content"`
		Reasoning        string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return ""
	}
	if probe.ReasoningContent != "" {
		return probe.ReasoningContent
	}
	return probe.Reasoning
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

func usageFromAcc(acc openai.ChatCompletionAccumulator, rawUsage string) Usage {
	cached, write, reported := cacheFromRawUsage(rawUsage)
	return Usage{
		PromptTokens:     acc.Usage.PromptTokens,
		CompletionTokens: acc.Usage.CompletionTokens,
		TotalTokens:      acc.Usage.TotalTokens,
		CachedTokens:     cached,
		CacheWriteTokens: write,
		CacheReported:    reported,
	}
}

// rawUsageCache mirrors the cache-accounting fields providers put in a usage
// object. OpenAI-compatible APIs nest them under prompt_tokens_details,
// DeepSeek reports prompt_cache_hit_tokens at the top level, and
// Anthropic-backed gateways report cache_read_input_tokens /
// cache_creation_input_tokens. Pointers distinguish "reported as zero" from
// "absent", which is what CacheReported reports.
type rawUsageCache struct {
	Details struct {
		Cached     *int64 `json:"cached_tokens"`
		CacheWrite *int64 `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CacheHit      *int64 `json:"prompt_cache_hit_tokens"`
	CacheRead     *int64 `json:"cache_read_input_tokens"`
	CacheCreation *int64 `json:"cache_creation_input_tokens"`
}

// cacheFromRawUsage reads cache accounting from a raw usage JSON object. The
// SDK's typed usage drops provider-specific fields and does not accumulate
// cache_write_tokens, so the streamed chunk's RawJSON is the source of truth.
// reported is false when no provider field is present.
func cacheFromRawUsage(raw string) (cached, write int64, reported bool) {
	if raw == "" {
		return 0, 0, false
	}
	var u rawUsageCache
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		return 0, 0, false
	}
	cached, reported = firstInt(u.Details.Cached, u.CacheHit, u.CacheRead)
	if w, ok := firstInt(u.Details.CacheWrite, u.CacheCreation); ok {
		write, reported = w, true
	}
	return cached, write, reported
}

// firstInt returns the first present value, so callers can try provider
// spellings in preference order.
func firstInt(vals ...*int64) (int64, bool) {
	for _, v := range vals {
		if v != nil {
			return *v, true
		}
	}
	return 0, false
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
//
// tools are sent verbatim with tool_choice "none". Providers fold the tool
// array into the cached request prefix, so passing a conversation's own tools
// keeps that prefix intact — the caller is reusing a warm prefix, not asking
// for a tool to run — while the choice keeps the model from calling one.
func (c *chatClient) complete(ctx context.Context, model, _ string, msgs []Message, tools []Tool, maxTokens int64) (string, error) {
	apiMsgs, err := toAPIMessages(msgs)
	if err != nil {
		return "", err
	}

	params := openai.ChatCompletionNewParams{
		Model:    model,
		Messages: apiMsgs,
	}
	if maxTokens > 0 {
		params.MaxCompletionTokens = openai.Int(maxTokens)
	}
	if len(tools) > 0 {
		params.Tools = toAPITools(tools)
		params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: openai.String(string(openai.ChatCompletionToolChoiceOptionAutoNone)),
		}
	}

	res, err := c.api.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", classifyErr(err)
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
			um, err := userParam(m)
			if err != nil {
				return nil, err
			}
			apiMsgs = append(apiMsgs, um)
		case RoleAssistant:
			apiMsgs = append(apiMsgs, assistantParam(m))
		case RoleSystem, RoleDeveloper:
			// developer is OpenAI-specific; map to system for compatible APIs.
			apiMsgs = append(apiMsgs, openai.SystemMessage(m.Text))
		case RoleTool:
			apiMsgs = append(apiMsgs, openai.ToolMessage(m.Text, m.ToolCallID))
		case RoleCompaction:
			// Opaque to Chat Completions providers; the harness rebuilds history
			// from the session log when the model changes, so this is a backstop.
			continue
		default:
			return nil, fmt.Errorf("unknown message role %q", m.Role)
		}
	}
	return apiMsgs, nil
}

func userParam(m Message) (openai.ChatCompletionMessageParamUnion, error) {
	if len(m.Images) == 0 {
		return openai.UserMessage(m.Text), nil
	}
	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, 1+len(m.Images))
	if m.Text != "" {
		parts = append(parts, openai.TextContentPart(m.Text))
	}
	for _, img := range m.Images {
		url := strings.TrimSpace(img.URL)
		if !strings.HasPrefix(url, "data:") {
			return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("image: missing data URL")
		}
		parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL: url,
		}))
	}
	if len(parts) == 0 {
		return openai.UserMessage(""), nil
	}
	return openai.UserMessage(parts), nil
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
