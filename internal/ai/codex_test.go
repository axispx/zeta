package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
)

// codexTestProvider is a Codex-backed provider with OAuth credentials.
func codexTestProvider(baseURL string) config.Provider {
	return config.Provider{
		BaseURL: baseURL,
		OAuth: &config.OAuthCredential{
			AccessToken:  "token-1",
			RefreshToken: "refresh-1",
			AccountID:    "acct-1",
		},
		Models: map[string]config.ModelDef{"gpt-5.5": {ContextWindow: 272000}},
	}
}

// sse writes one server-sent event frame.
func sse(w http.ResponseWriter, payload string) {
	fmt.Fprintf(w, "event: x\ndata: %s\n\n", payload)
}

// codexClientFor builds a Responses client for base. Tests reach past New's
// base-URL dispatch, which TestNewTransport covers on its own.
func codexClientFor(base string) *Client {
	return &Client{api: newCodexClient(codexTestProvider(base)), model: "gpt-5.5"}
}

func TestCodexRequestShape(t *testing.T) {
	t.Parallel()
	msgs := []Message{
		{Role: RoleSystem, Text: "you are zeta"},
		{Role: RoleDeveloper, Text: "build mode"},
		{Role: RoleUser, Text: "hi"},
		{Role: RoleAssistant, Text: "let me look", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "read", Arguments: `{"path":"a.go"}`},
			// An empty arguments object must survive as "{}", not be omitted.
			{ID: "call_2", Name: "todo", Arguments: ""},
		}},
		{Role: RoleTool, Text: "contents", ToolCallID: "call_1"},
	}
	tools := []Tool{{Name: "read", Description: "read a file", Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
	}}}

	c := newCodexClient(codexTestProvider("https://chatgpt.com/backend-api/codex"))
	req, err := c.requestBody("gpt-5.5", "high", msgs, tools, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if req.Instructions != "you are zeta\n\nbuild mode" {
		t.Fatalf("instructions = %q", req.Instructions)
	}
	if req.Store || !req.Stream || req.ToolChoice != "auto" {
		t.Fatalf("request = %#v", req)
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "high" || req.Reasoning.Summary != "auto" {
		t.Fatalf("reasoning = %#v", req.Reasoning)
	}
	// Raw JSON keeps the pointer-encoded arguments and the item order.
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{
		`"type":"function_call"`, `"call_id":"call_1"`, `"arguments":"{\"path\":\"a.go\"}"`,
		`"arguments":""`, `"type":"function_call_output"`, `"output":"contents"`,
		`"type":"input_text"`, `"output_text"`, `"store":false`, `"stream":true`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s:\n%s", want, body)
		}
	}
	// System/developer must not also appear as input items.
	if strings.Contains(body, `"role":"system"`) || strings.Contains(body, `"role":"developer"`) {
		t.Fatalf("system/developer belong in instructions:\n%s", body)
	}
}

// A non-streaming request sends the conversation's tools with tool_choice none
// and no reasoning summary, matching the Chat Completions summarizer.
func TestCodexRequestBody(t *testing.T) {
	t.Parallel()
	c := newCodexClient(codexTestProvider("https://chatgpt.com/backend-api/codex"))
	tools := []Tool{{Name: "read"}}
	req, err := c.requestBody("m", "", []Message{{Role: RoleUser, Text: "hi"}}, tools, 128, false)
	if err != nil {
		t.Fatal(err)
	}
	if req.Stream || req.ToolChoice != "none" || req.MaxOutputTokens != 128 {
		t.Fatalf("request = %#v", req)
	}
	if req.Reasoning == nil || req.Reasoning.Summary != "" || req.Reasoning.Effort != "" {
		t.Fatalf("reasoning = %#v", req.Reasoning)
	}
	// A tool-less streaming request omits tool_choice entirely.
	req, err = c.requestBody("m", "", []Message{{Role: RoleUser, Text: "hi"}}, nil, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if req.ToolChoice != "" || len(req.Tools) != 0 {
		t.Fatalf("request = %#v", req)
	}

	if _, err := c.requestBody("m", "", []Message{{Role: "bogus"}}, nil, 0, true); err == nil {
		t.Fatal("expected unknown-role error")
	}
}

func TestCodexInputImages(t *testing.T) {
	t.Parallel()
	msgs := []Message{{Role: RoleUser, Images: []Image{{URL: "data:image/png;base64,AAA"}}}}
	_, input, err := codexInput(msgs)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type":"input_image"`) {
		t.Fatalf("images must map to input_image: %s", raw)
	}
}

// streamCodex runs one streaming turn against a handler and returns the events.
func streamCodex(t *testing.T, handler http.HandlerFunc, msgs []Message, tools []Tool) []Event {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	var out []Event
	for evt := range codexClientFor(srv.URL).Stream(context.Background(), msgs, tools) {
		out = append(out, evt)
	}
	return out
}

func TestCodexStreamTextAndToolCalls(t *testing.T) {
	var gotPath, gotAuth, gotAccount, gotOriginator string
	events := streamCodex(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		gotOriginator = r.Header.Get("originator")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-codex-primary-used-percent", "42.5")
		w.Header().Set("x-codex-primary-window-minutes", "300")
		w.Header().Set("x-codex-primary-reset-at", "1704069000")
		w.Header().Set("x-codex-secondary-used-percent", "10")
		w.Header().Set("x-codex-secondary-window-minutes", "10080")
		sse(w, `{"type":"response.output_text.delta","delta":"hel"}`)
		sse(w, `{"type":"response.output_text.delta","delta":"lo"}`)
		sse(w, `{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`)
		// Unknown event types must be ignored, not fatal.
		sse(w, `{"type":"response.some_future_event","delta":"ignored"}`)
		sse(w, `{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read","arguments":"{\"path\":\"a\"}"}}`)
		// Non-function items (messages, reasoning) are not tool calls.
		sse(w, `{"type":"response.output_item.done","item":{"type":"message","id":"msg_1"}}`)
		sse(w, `{"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":80}}}}`)
	}, []Message{{Role: RoleUser, Text: "hi"}}, nil)

	var text, reasoning string
	var done *Event
	for i := range events {
		switch events[i].Type {
		case EventDelta:
			text += events[i].Text
		case EventReasoning:
			reasoning += events[i].Text
		case EventDone:
			done = &events[i]
		case EventErr:
			t.Fatalf("unexpected error: %v", events[i].Err)
		}
	}
	if text != "hello" || reasoning != "thinking" {
		t.Fatalf("text=%q reasoning=%q", text, reasoning)
	}
	if gotPath != codex.ResponsesPath || gotAuth != "Bearer token-1" || gotAccount != "acct-1" || gotOriginator == "" {
		t.Fatalf("path=%q auth=%q account=%q originator=%q", gotPath, gotAuth, gotAccount, gotOriginator)
	}
	if done == nil {
		t.Fatal("no done event")
	}
	if done.Message.Text != "hello" || len(done.Message.ToolCalls) != 1 {
		t.Fatalf("message = %#v", done.Message)
	}
	if tc := done.Message.ToolCalls[0]; tc.ID != "call_1" || tc.Name != "read" || tc.Arguments != `{"path":"a"}` {
		t.Fatalf("tool call = %#v", tc)
	}
	if done.Usage != (Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CachedTokens: 80, CacheReported: true}) {
		t.Fatalf("usage = %#v", done.Usage)
	}
	// The quota rides the same response as the completion.
	plan := done.Plan
	if plan.Empty() || plan.Primary == nil || plan.Primary.UsedPercent != 42.5 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Primary.WindowMinutes != 300 || plan.Primary.ResetsAt != 1704069000 {
		t.Fatalf("primary window = %#v", plan.Primary)
	}
	if plan.Secondary == nil || plan.Secondary.WindowMinutes != 10080 {
		t.Fatalf("secondary window = %#v", plan.Secondary)
	}
}

func TestCodexStreamErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		status  int
		body    string
		wantSub string
		auth    bool
	}{
		{"unauthorized", http.StatusUnauthorized, `{"error":"bad token"}`, "status 401", true},
		{"forbidden", http.StatusForbidden, `{"error":"no plan"}`, "no plan", false},
		{"empty body", http.StatusBadGateway, "", "status 502", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events := streamCodex(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}, []Message{{Role: RoleUser, Text: "hi"}}, nil)
			if len(events) != 1 || events[0].Type != EventErr {
				t.Fatalf("events = %#v", events)
			}
			err := events[0].Err
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want %q", err, tc.wantSub)
			}
			// A 401 must stay recoverable by the OAuth retry path.
			if got := errors.Is(err, ErrAuth); got != tc.auth {
				t.Fatalf("ErrAuth = %v, want %v (%v)", got, tc.auth, err)
			}
		})
	}
}

func TestCodexStreamFailureEvents(t *testing.T) {
	t.Parallel()
	// response.failed carries the error under response.error.
	events := streamCodex(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w, `{"type":"response.failed","response":{"error":{"code":"usage_limit_reached","message":"quota spent"}}}`)
	}, []Message{{Role: RoleUser, Text: "hi"}}, nil)
	if len(events) != 1 || events[0].Type != EventErr {
		t.Fatalf("events = %#v", events)
	}
	if !strings.Contains(events[0].Err.Error(), "usage_limit_reached") {
		t.Fatalf("err = %v", events[0].Err)
	}

	// A bare error event reports its own error object.
	events = streamCodex(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w, `{"type":"error","error":{"message":"boom"}}`)
	}, []Message{{Role: RoleUser, Text: "hi"}}, nil)
	if len(events) != 1 || events[0].Type != EventErr || !strings.Contains(events[0].Err.Error(), "boom") {
		t.Fatalf("events = %#v", events)
	}
}

func TestCodexStreamMalformedFrame(t *testing.T) {
	t.Parallel()
	// A frame zeta cannot parse is skipped; the rest of the turn still lands.
	events := streamCodex(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w, `{not json`)
		sse(w, `{"type":"response.output_text.delta","delta":"ok"}`)
		sse(w, `{"type":"response.completed","response":{}}`)
	}, []Message{{Role: RoleUser, Text: "hi"}}, nil)
	if len(events) != 2 || events[0].Type != EventDelta || events[0].Text != "ok" || events[1].Type != EventDone {
		t.Fatalf("events = %#v", events)
	}
}

func TestCodexStreamCancelled(t *testing.T) {
	t.Parallel()
	// The handler holds the response open like a slow turn would. It must not
	// outlive the test: srv.Close waits for handlers, and the request context
	// alone is not a reliable signal that the client went away.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"type":"response.output_text.delta","delta":"partial"}`)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	ch := codexClientFor(srv.URL).Stream(ctx, []Message{{Role: RoleUser, Text: "hi"}}, nil)
	<-ch // first delta
	cancel()
	// The stream must terminate (with Done, not Err) rather than hang.
	for evt := range ch {
		if evt.Type == EventErr {
			t.Fatalf("cancellation must not surface an error: %v", evt.Err)
		}
	}
}

// The quota rides the streaming response's headers. The transport must not
// fetch anything itself: a slow usage endpoint would otherwise delay the first
// token. /usage reads the fallback endpoint on demand instead.
func TestCodexNoPlanFetchDuringStream(t *testing.T) {
	t.Parallel()
	var paths []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"type":"response.completed","response":{}}`)
	})

	events := streamCodex(t, handler, []Message{{Role: RoleUser, Text: "hi"}}, nil)
	done := events[len(events)-1]
	if done.Type != EventDone {
		t.Fatalf("events = %#v", events)
	}
	if done.Plan != nil {
		t.Fatalf("no headers means no quota on the response: %#v", done.Plan)
	}
	if len(paths) != 1 || paths[0] != codex.ResponsesPath {
		t.Fatalf("stream must make exactly one request, got %v", paths)
	}
}

func TestCodexComplete(t *testing.T) {
	t.Parallel()
	var gotStore bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotStore = body.Stream
		_, _ = w.Write([]byte(`{"output":[{"type":"reasoning"},{"type":"message","content":[{"type":"output_text","text":"a title"}]}]}`))
	}))
	t.Cleanup(srv.Close)

	c := codexClientFor(srv.URL)
	got, err := c.Complete(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil, 32)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a title" {
		t.Fatalf("completion = %q", got)
	}
	if gotStore {
		t.Fatal("Complete must not stream")
	}

	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
	}))
	t.Cleanup(srvErr.Close)
	c = codexClientFor(srvErr.URL)
	if _, err := c.Complete(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil, 32); err == nil {
		t.Fatal("expected error body to surface")
	}

	c = codexClientFor("http://127.0.0.1:1")
	if _, err := c.Complete(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil, 32); err == nil {
		t.Fatal("expected transport error")
	}
}

// New must route the Codex backend to the Responses transport and everything
// else to Chat Completions.
func TestNewTransport(t *testing.T) {
	t.Parallel()
	codexProvider := codexTestProvider("https://chatgpt.com/backend-api/codex")
	if _, ok := New(codexProvider, "gpt-5.5").api.(*codexClient); !ok {
		t.Fatal("codex backend must use the Responses transport")
	}
	// A platform API-key endpoint is Chat Completions, even on the same host.
	apiProvider := config.Provider{
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-x",
		Models:  map[string]config.ModelDef{"gpt-5.5": {ContextWindow: 272000}},
	}
	if _, ok := New(apiProvider, "gpt-5.5").api.(*chatClient); !ok {
		t.Fatal("api.openai.com must stay on Chat Completions")
	}
}

func TestSSEFrames(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		": keep-alive",
		"event: response.output_text.delta",
		"data: {\"a\":1}",
		"",
		"data: line one",
		"data: line two",
		"",
		"data: no trailing blank line",
	}, "\n")
	f := newSSEFrames(strings.NewReader(body))
	want := []string{`{"a":1}`, "line oneline two", "no trailing blank line"}
	for i, w := range want {
		got, ok, err := f.next()
		if err != nil || !ok {
			t.Fatalf("frame %d: ok=%v err=%v", i, ok, err)
		}
		if string(got) != w {
			t.Fatalf("frame %d = %q, want %q", i, got, w)
		}
	}
	if _, ok, err := f.next(); ok || err != nil {
		t.Fatalf("expected clean EOF, got ok=%v err=%v", ok, err)
	}
}

// dropConn closes the connection without answering, the way a dead socket
// looks to the client.
func dropConn(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	_ = conn.Close()
}

// A connection that dies before any response is resent; the caller sees only
// the eventual success.
func TestCodexResendsDroppedConnection(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= codexSendRetries {
			dropConn(t, w)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"type":"response.output_text.delta","delta":"ok"}`)
		sse(w, `{"type":"response.completed","response":{}}`)
	}))
	t.Cleanup(srv.Close)

	cc := newCodexClient(codexTestProvider(srv.URL))
	cc.backoff = time.Millisecond
	c := &Client{api: cc, model: "gpt-5.5"}
	var text strings.Builder
	for ev := range c.Stream(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil) {
		switch ev.Type {
		case EventErr:
			t.Fatalf("unexpected error: %v", ev.Err)
		case EventDelta:
			text.WriteString(ev.Text)
		}
	}
	if text.String() != "ok" {
		t.Fatalf("text = %q", text.String())
	}
	if got := calls.Load(); got != codexSendRetries+1 {
		t.Fatalf("calls = %d, want %d", got, codexSendRetries+1)
	}
}

// Retries are bounded, and an HTTP error status is never resent.
func TestCodexResendLimits(t *testing.T) {
	t.Parallel()
	var drops, rejects atomic.Int32
	dropper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		drops.Add(1)
		dropConn(t, w)
	}))
	t.Cleanup(dropper.Close)
	rejecter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rejects.Add(1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(rejecter.Close)

	for _, srv := range []*httptest.Server{dropper, rejecter} {
		cc := newCodexClient(codexTestProvider(srv.URL))
		cc.backoff = time.Millisecond
		c := &Client{api: cc, model: "gpt-5.5"}
		if _, err := c.Complete(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil, 32); err == nil {
			t.Fatal("expected error")
		}
	}
	if got := drops.Load(); got != codexSendRetries+1 {
		t.Fatalf("dropped attempts = %d, want %d", got, codexSendRetries+1)
	}
	if got := rejects.Load(); got != 1 {
		t.Fatalf("rejected attempts = %d, want 1", got)
	}
}

func TestTransientNetErr(t *testing.T) {
	t.Parallel()
	wrap := func(err error) error {
		return &url.Error{Op: "Post", URL: "https://x", Err: &net.OpError{Op: "read", Err: os.NewSyscallError("read", err)}}
	}
	for _, err := range []error{syscall.ETIMEDOUT, syscall.ECONNRESET, syscall.EPIPE} {
		if !transientNetErr(wrap(err)) {
			t.Errorf("%v should be transient", err)
		}
	}
	for _, err := range []error{syscall.ECONNREFUSED, context.Canceled, context.DeadlineExceeded} {
		if transientNetErr(wrap(err)) {
			t.Errorf("%v should not be transient", err)
		}
	}
	if !transientNetErr(fmt.Errorf("Post: %w", io.EOF)) {
		t.Error("EOF should be transient")
	}
}
