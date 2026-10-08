package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/oauth"
)

// maxFrameBytes bounds one SSE frame: encrypted reasoning payloads and large
// tool arguments arrive as a single data line.
const maxFrameBytes = 8 << 20

// codexRequestTimeout bounds a complete() request. Turn streams carry
// no client timeout — a long turn is not a failure — so ctx cancellation is
// what stops them.
const codexRequestTimeout = 2 * time.Minute

// codexSendRetries is how many times a request is resent after a transient
// network failure before any response arrived, matching the SDK's default on
// the Chat Completions path. codexRetryBackoff is the wait before the first
// resend; it doubles each time.
const (
	codexSendRetries  = 2
	codexRetryBackoff = time.Second
)

// codexClient runs Responses API requests against the ChatGPT Codex backend.
//
// The backend takes only Responses shapes: typed input items (function_call /
// function_call_output rather than messages carrying tool_calls) and a
// different SSE schema, so this is a transport of its own rather than a base
// URL swap on the Chat Completions client.
//
// Reasoning items are deliberately not echoed back. zeta's JSONL transcript is
// the durable conversation and holds no reasoning, and a stateless request that
// sends each function call with its output is accepted without them.
//
// The transport does no I/O of its own: the account's plan quota rides the
// response headers, and a backend that reports none is read on demand by
// /usage (harness.PlanQuota) rather than on the token path.
type codexClient struct {
	http      *http.Client
	streaming *http.Client
	baseURL   string
	token     string
	accountID string
	// serviceTier is sent as service_tier ("priority" is fast mode); empty
	// omits it. It rides every request, summarizer included, so the cached
	// prefix is served the same way it was written.
	serviceTier string
	backoff     time.Duration // first resend delay; tests shrink it
}

func newCodexClient(p config.Provider) *codexClient {
	accountID := ""
	if p.OAuth != nil {
		accountID = strings.TrimSpace(p.OAuth.AccountID)
	}
	return &codexClient{
		http:      &http.Client{Timeout: codexRequestTimeout},
		streaming: &http.Client{},
		baseURL:   strings.TrimRight(strings.TrimSpace(p.BaseURL), "/"),
		token:     p.AuthToken(),
		accountID: accountID,
		backoff:   codexRetryBackoff,
	}
}

// codexRequest is the Responses API request body. Store is always false: zeta
// keeps the conversation, not OpenAI.
type codexRequest struct {
	Model             string          `json:"model"`
	Instructions      string          `json:"instructions,omitempty"`
	Input             []any           `json:"input"`
	Tools             []codexTool     `json:"tools,omitempty"`
	ToolChoice        string          `json:"tool_choice,omitempty"`
	ParallelToolCalls bool            `json:"parallel_tool_calls"`
	Reasoning         *codexReasoning `json:"reasoning,omitempty"`
	ServiceTier       string          `json:"service_tier,omitempty"`
	Store             bool            `json:"store"`
	Stream            bool            `json:"stream"`
}

// codexReasoning asks for a reasoning summary so the TUI can show thinking the
// way it does for providers that stream reasoning deltas.
type codexReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type codexTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

type codexInputItem struct {
	Type    string         `json:"type"`
	Role    string         `json:"role,omitempty"`
	Content []codexContent `json:"content,omitempty"`
	CallID  string         `json:"call_id,omitempty"`
	Name    string         `json:"name,omitempty"`
	// Arguments is a JSON string, so a pointer keeps `{}` distinct from absent
	// (the API rejects a function_call without arguments). Output is a pointer
	// for the same reason: a tool that printed nothing still needs `"output":""`
	// (the API rejects a function_call_output without one).
	Arguments *string `json:"arguments,omitempty"`
	Output    *string `json:"output,omitempty"`
}

type codexContent struct {
	Type     string `json:"type"` // input_text | output_text | input_image
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// requestBody assembles the request for msgs. tools may be empty. The request
// carries no output cap: the ChatGPT Codex backend does not take one (the Codex
// CLI sends none), so a caller's limit is enforced on the returned text instead.
func (c *codexClient) requestBody(model, effort string, msgs []Message, tools []Tool, stream bool) (*codexRequest, error) {
	instructions, input, err := codexInput(msgs)
	if err != nil {
		return nil, err
	}
	req := &codexRequest{
		Model:        model,
		Instructions: instructions,
		Input:        input,
		Reasoning:    &codexReasoning{Effort: effort},
		ServiceTier:  c.serviceTier,
		Store:        false,
		Stream:       stream,
	}
	if stream {
		req.Reasoning.Summary = "auto"
	}
	if len(tools) > 0 {
		req.Tools = codexTools(tools)
		if stream {
			req.ToolChoice = "auto"
		} else {
			// Mirrors the Chat Completions path: the conversation's own tools
			// ride along to keep the cached prefix intact, but the model must
			// not call one on a summarization request.
			req.ToolChoice = "none"
		}
	}
	return req, nil
}

// codexInput maps zeta messages onto the Responses API input. System and
// developer messages become the request's instructions: they head the cached
// prefix, and the API has a single such slot.
func codexInput(msgs []Message) (string, []any, error) {
	var instructions []string
	input := make([]any, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case RoleSystem, RoleDeveloper:
			if text := strings.TrimSpace(m.Text); text != "" {
				instructions = append(instructions, text)
			}
		case RoleUser:
			input = append(input, codexInputItem{Type: "message", Role: "user", Content: codexUserContent(m)})
		case RoleAssistant:
			if m.Text != "" {
				input = append(input, codexInputItem{
					Type:    "message",
					Role:    "assistant",
					Content: []codexContent{{Type: "output_text", Text: m.Text}},
				})
			}
			for _, tc := range m.ToolCalls {
				args := tc.Arguments
				input = append(input, codexInputItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Name,
					Arguments: &args,
				})
			}
		case RoleTool:
			output := m.Text
			input = append(input, codexInputItem{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: &output,
			})
		default:
			return "", nil, fmt.Errorf("unknown message role %q", m.Role)
		}
	}
	return strings.Join(instructions, "\n\n"), input, nil
}

func codexUserContent(m Message) []codexContent {
	out := make([]codexContent, 0, 1+len(m.Images))
	if m.Text != "" {
		out = append(out, codexContent{Type: "input_text", Text: m.Text})
	}
	for _, img := range m.Images {
		out = append(out, codexContent{Type: "input_image", ImageURL: img.URL})
	}
	if len(out) == 0 {
		out = append(out, codexContent{Type: "input_text", Text: ""})
	}
	return out
}

func codexTools(tools []Tool) []codexTool {
	out := make([]codexTool, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, codexTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}
	return out
}

func (c *codexClient) stream(ctx context.Context, model, effort string, msgs []Message, tools []Tool, out chan<- Event) {
	req, err := c.requestBody(model, effort, msgs, tools, true)
	if err != nil {
		out <- Event{Type: EventErr, Err: err}
		return
	}
	resp, err := c.do(ctx, c.streaming, req)
	if err != nil {
		if ctx.Err() != nil {
			out <- Event{Type: EventDone}
			return
		}
		out <- Event{Type: EventErr, Err: err}
		return
	}
	defer resp.Body.Close()

	// Free: the backend meters the account on these headers.
	plan := codex.PlanFromHeaders(resp.Header)
	acc := codexAccumulator{}
	frames := newSSEFrames(resp.Body)
	for {
		payload, ok, err := frames.next()
		if err != nil {
			if ctx.Err() != nil {
				out <- Event{Type: EventDone}
				return
			}
			out <- Event{Type: EventErr, Err: fmt.Errorf("codex stream: %w", err)}
			return
		}
		if !ok {
			break
		}
		for _, evt := range acc.consume(payload) {
			out <- evt
		}
	}
	if acc.err != nil {
		out <- Event{Type: EventErr, Err: acc.err}
		return
	}
	out <- Event{Type: EventDone, Message: acc.message(), Usage: acc.usage, Plan: plan}
}

// complete runs a short, tool-free request for titles and compaction summaries.
// The backend only serves streams (it rejects stream:false), so the reply is
// read off the wire as a stream and joined; the request still carries the
// summarizer shape (tool_choice none, no reasoning summary) and the client
// timeout, since nothing here reaches the UI.
func (c *codexClient) complete(ctx context.Context, model, effort string, msgs []Message, tools []Tool, _ int64) (string, error) {
	req, err := c.requestBody(model, effort, msgs, tools, false)
	if err != nil {
		return "", err
	}
	req.Stream = true
	resp, err := c.do(ctx, c.http, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	acc := codexAccumulator{}
	frames := newSSEFrames(resp.Body)
	for {
		payload, ok, err := frames.next()
		if err != nil {
			return "", fmt.Errorf("codex stream: %w", err)
		}
		if !ok {
			break
		}
		acc.consume(payload)
	}
	if acc.err != nil {
		return "", acc.err
	}
	return acc.text.String(), nil
}

// do posts body to the Responses endpoint and returns a successful response.
// Failures are classified so a 401 still drives the OAuth recovery path. A
// transient network failure (see transientNetErr) is resent a few times: it
// happens before any response, so nothing has streamed to the user yet, and
// the usual cause is a dead pooled connection that a fresh one replaces.
func (c *codexClient) do(ctx context.Context, client *http.Client, body *codexRequest) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var resp *http.Response
	backoff := c.backoff
	for attempt := 0; ; attempt++ {
		resp, err = c.send(ctx, client, payload)
		if err == nil {
			break
		}
		if attempt >= codexSendRetries || ctx.Err() != nil || !transientNetErr(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	return nil, codexStatusError(resp.StatusCode, detail)
}

// send makes one attempt at posting payload.
func (c *codexClient) send(ctx context.Context, client *http.Client, payload []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+codex.ResponsesPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.token)
	// The backend gates ChatGPT-plan usage on the originator identity, and
	// needs the account id the tokens are scoped to.
	req.Header.Set("originator", oauth.CodexOriginator)
	req.Header.Set("User-Agent", "zeta")
	if c.accountID != "" {
		req.Header.Set("chatgpt-account-id", c.accountID)
	}
	return client.Do(req)
}

// transientNetErr reports a connection that died underneath a request: the
// kernel gave up retransmitting (ETIMEDOUT), the peer reset or closed it, or
// it ended before a response. Client-side timeouts are excluded — resending
// a request that already ran for the full codexRequestTimeout only triples
// the wait — and so are refused connections and TLS or DNS failures, which a
// retry a second later does not fix.
func transientNetErr(err error) bool {
	return errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}

// codexStatusError renders a backend failure, keeping 401 recoverable.
func codexStatusError(status int, detail []byte) error {
	msg := strings.TrimSpace(string(detail))
	if msg == "" {
		msg = http.StatusText(status)
	}
	err := fmt.Errorf("codex: %s (status %d)", msg, status)
	if status == http.StatusUnauthorized {
		return fmt.Errorf("%w: %v", ErrAuth, err)
	}
	return err
}

// codexAccumulator folds streamed Responses events into a Message and Usage.
type codexAccumulator struct {
	text      strings.Builder
	toolCalls []ToolCall
	usage     Usage
	err       error
}

// codexEvent is the subset of a Responses stream event zeta reads. Unknown
// event types are ignored, which is what lets the client keep working as the
// backend adds events.
type codexEvent struct {
	Type     string            `json:"type"`
	Delta    string            `json:"delta"`
	Item     *codexOutputItem  `json:"item"`
	Response *codexResponse    `json:"response"`
	Error    *codexErrorDetail `json:"error"`
}

type codexOutputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type codexResponse struct {
	Usage *codexUsage       `json:"usage"`
	Error *codexErrorDetail `json:"error"`
}

type codexErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *codexErrorDetail) text() string {
	if e == nil {
		return "unknown error"
	}
	switch {
	case e.Code != "" && e.Message != "":
		return e.Code + ": " + e.Message
	case e.Message != "":
		return e.Message
	case e.Code != "":
		return e.Code
	}
	return "unknown error"
}

type codexUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
	// Absent on requests where the backend reports no cache accounting, which
	// is what CacheReported tracks.
	InputTokensDetails struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

// consume folds one SSE payload and returns the events it produces.
func (a *codexAccumulator) consume(payload []byte) []Event {
	var evt codexEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		// A frame zeta cannot read is not fatal: the stream keeps flowing and
		// response.completed still lands.
		return nil
	}
	switch evt.Type {
	case "response.output_text.delta":
		if evt.Delta != "" {
			a.text.WriteString(evt.Delta)
			return []Event{{Type: EventDelta, Text: evt.Delta}}
		}
	case "response.reasoning_summary_text.delta":
		if evt.Delta != "" {
			return []Event{{Type: EventReasoning, Text: evt.Delta}}
		}
	case "response.output_item.done":
		if evt.Item != nil {
			a.addToolCall(*evt.Item)
		}
	case "response.completed":
		a.setUsage(evt.Response)
		// The server closes the stream after this; the loop ends on EOF.
	case "response.failed", "response.incomplete":
		detail := evt.Error
		if evt.Response != nil && evt.Response.Error != nil {
			detail = evt.Response.Error
		}
		a.err = errors.New("codex: " + detail.text())
	case "error":
		a.err = errors.New("codex: " + evt.Error.text())
	}
	return nil
}

func (a *codexAccumulator) addToolCall(item codexOutputItem) {
	if item.Type != "function_call" {
		return
	}
	// function_call_output pairs by call_id, so an item without one cannot be
	// answered; skip it rather than send an orphan result. The item id is the
	// fallback for providers that populate only that.
	id := item.CallID
	if id == "" {
		id = item.ID
	}
	if id == "" || item.Name == "" {
		return
	}
	a.toolCalls = append(a.toolCalls, ToolCall{
		ID:        id,
		Name:      item.Name,
		Arguments: item.Arguments,
	})
}

func (a *codexAccumulator) setUsage(res *codexResponse) {
	if res == nil || res.Usage == nil {
		return
	}
	u := res.Usage
	usage := Usage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
	}
	if c := u.InputTokensDetails.CachedTokens; c != nil {
		usage.CachedTokens = *c
		usage.CacheReported = true
	}
	a.usage = usage
}

func (a *codexAccumulator) message() Message {
	return Message{
		Role:      RoleAssistant,
		Text:      a.text.String(),
		ToolCalls: a.toolCalls,
	}
}

// sseFrames yields the data payload of each server-sent event frame.
type sseFrames struct {
	sc   *bufio.Scanner
	data strings.Builder
	done bool
}

func newSSEFrames(r io.Reader) *sseFrames {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxFrameBytes)
	return &sseFrames{sc: sc}
}

// next returns the next data payload. ok is false once the body ends.
func (f *sseFrames) next() (payload []byte, ok bool, err error) {
	if f.done {
		return nil, false, nil
	}
	for f.sc.Scan() {
		line := f.sc.Text()
		switch {
		case line == "":
			if f.data.Len() == 0 {
				continue
			}
			return f.take(), true, nil
		case strings.HasPrefix(line, "data:"):
			f.data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		default:
			// event:/id:/retry: lines and comments. The payload names its own
			// type, so nothing else is needed.
		}
	}
	f.done = true
	if err := f.sc.Err(); err != nil {
		return nil, false, err
	}
	if f.data.Len() > 0 {
		return f.take(), true, nil
	}
	return nil, false, nil
}

func (f *sseFrames) take() []byte {
	out := []byte(f.data.String())
	f.data.Reset()
	return out
}
