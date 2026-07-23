package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSearchProvider(t *testing.T) {
	t.Setenv("ZETA_WEBSEARCH_PROVIDER", "parallel")
	if got := webSearchProvider(); got != "parallel" {
		t.Fatalf("override: %q", got)
	}
	t.Setenv("ZETA_WEBSEARCH_PROVIDER", "")
	if got := webSearchProvider(); got != "exa" {
		t.Fatalf("default: %q", got)
	}
	t.Setenv("ZETA_WEBSEARCH_PROVIDER", "EXA")
	if got := webSearchProvider(); got != "exa" {
		t.Fatalf("exa: %q", got)
	}
}

func TestParseMCPResponse(t *testing.T) {
	payload := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hello"}]}}`
	text, errMsg := parseMCPResponse(payload)
	if text != "hello" || errMsg != "" {
		t.Fatalf("direct: text=%q err=%q", text, errMsg)
	}

	sse := "data: " + payload
	text, errMsg = parseMCPResponse(sse)
	if text != "hello" || errMsg != "" {
		t.Fatalf("sse: text=%q err=%q", text, errMsg)
	}

	errPayload := `{"jsonrpc":"2.0","error":{"code":-32000,"message":"You've hit Exa's free MCP rate limit"}}`
	_, errMsg = parseMCPResponse(errPayload)
	if !isRateLimitMessage(errMsg) {
		t.Fatalf("rate limit: %q", errMsg)
	}
}

func TestWebsearchMCPSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcpRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Params.Name != "web_search_exa" {
			t.Fatalf("tool: %s", req.Params.Name)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"result one"}]}}`))
	}))
	defer srv.Close()

	text, err := callWebSearchMCP(context.Background(), "exa", srv.URL, "web_search_exa", map[string]any{
		"query": "golang", "type": "auto", "numResults": 3, "livecrawl": "fallback",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "result one" {
		t.Fatalf("got %q", text)
	}
}

func TestWebsearchRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := callWebSearchMCP(context.Background(), "exa", srv.URL, "web_search_exa", map[string]any{"query": "x"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "EXA_API_KEY") {
		t.Fatalf("got %v", err)
	}
}

func TestWebsearchRunValidation(t *testing.T) {
	out, err := (websearchTool{}).Run(context.Background(), "", mustRaw(t, map[string]any{"query": ""}))
	if err == nil || out != "" {
		t.Fatalf("err=%v out=%q", err, out)
	}
}

func TestWebsearchSummary(t *testing.T) {
	got := (websearchTool{}).Summary(mustRaw(t, map[string]any{"query": "golang generics"}))
	if got != `websearch "golang generics"` {
		t.Fatal(got)
	}
}
