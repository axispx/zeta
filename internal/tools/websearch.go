package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	exaMCPBaseURL      = "https://mcp.exa.ai/mcp"
	parallelMCPBaseURL = "https://search.parallel.ai/mcp"
	webSearchTimeout   = 25 * time.Second
	maxWebSearchBytes  = 512 * 1024
	defaultNumResults  = 8
)

type websearchTool struct{}

func (websearchTool) Name() string { return "websearch" }
func (websearchTool) Description() string {
	return "Search the web for up-to-date information beyond the model's knowledge cutoff. " +
		"Returns titles, URLs, and snippets from relevant pages."
}
func (websearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
		},
		"required": []string{"query"},
	}
}

func (websearchTool) Summary(raw json.RawMessage) string {
	var a struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(raw, &a)
	q := strings.TrimSpace(a.Query)
	if q == "" {
		return "websearch"
	}
	if len(q) > 60 {
		q = q[:57] + "..."
	}
	return "websearch " + strconv.Quote(q)
}

func (websearchTool) Run(ctx context.Context, _ string, raw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	text, err := runWebSearch(ctx, webSearchProvider(), query)
	if err != nil {
		return "", err
	}
	if len(text) > maxWebSearchBytes {
		text = text[:maxWebSearchBytes] + "\n\n[truncated]"
	}
	return text, nil
}

func webSearchProvider() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ZETA_WEBSEARCH_PROVIDER"))) {
	case "parallel":
		return "parallel"
	default:
		return "exa"
	}
}

func runWebSearch(ctx context.Context, provider, query string) (string, error) {
	switch provider {
	case "parallel":
		headers := map[string]string{"User-Agent": "zeta"}
		if key := strings.TrimSpace(os.Getenv("PARALLEL_API_KEY")); key != "" {
			headers["Authorization"] = "Bearer " + key
		}
		return callWebSearchMCP(ctx, "parallel", parallelMCPBaseURL, "web_search", map[string]any{
			"objective":      query,
			"search_queries": []string{query},
		}, headers)
	default:
		return callWebSearchMCP(ctx, "exa", exaMCPURL(), "web_search_exa", map[string]any{
			"query":      query,
			"type":       "auto",
			"numResults": defaultNumResults,
			"livecrawl":  "fallback",
		}, nil)
	}
}

func exaMCPURL() string {
	key := strings.TrimSpace(os.Getenv("EXA_API_KEY"))
	if key == "" {
		return exaMCPBaseURL
	}
	u, err := url.Parse(exaMCPBaseURL)
	if err != nil {
		return exaMCPBaseURL
	}
	q := u.Query()
	q.Set("exaApiKey", key)
	u.RawQuery = q.Encode()
	return u.String()
}

type mcpRequest struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      int       `json:"id"`
	Method  string    `json:"method"`
	Params  mcpParams `json:"params"`
}

type mcpParams struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

type mcpResult struct {
	Result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func callWebSearchMCP(ctx context.Context, provider, mcpURL, tool string, args any, headers map[string]string) (string, error) {
	body, err := json.Marshal(mcpRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  mcpParams{Name: tool, Arguments: args},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := (&http.Client{Timeout: webSearchTimeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxWebSearchBytes))
	if err != nil {
		return "", err
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", webSearchRateLimitError(provider)
	}

	text, mcpErr := parseMCPResponse(string(raw))
	if mcpErr != "" {
		if isRateLimitMessage(mcpErr) {
			return "", webSearchRateLimitError(provider)
		}
		return "", fmt.Errorf("%s", mcpErr)
	}
	if text == "" {
		return "", fmt.Errorf("empty response from %s", provider)
	}
	return text, nil
}

func parseMCPResponse(body string) (text, errMsg string) {
	if text, errMsg = parseMCPPayload(strings.TrimSpace(body)); text != "" || errMsg != "" {
		return text, errMsg
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if text, errMsg = parseMCPPayload(strings.TrimSpace(line[6:])); text != "" || errMsg != "" {
			return text, errMsg
		}
	}
	return "", ""
}

func parseMCPPayload(payload string) (text, errMsg string) {
	if payload == "" || payload[0] != '{' {
		return "", ""
	}
	var data mcpResult
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return "", ""
	}
	if data.Error != nil && data.Error.Message != "" {
		return "", data.Error.Message
	}
	for _, item := range data.Result.Content {
		if item.Text != "" {
			return item.Text, ""
		}
	}
	return "", ""
}

func isRateLimitMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "rate limit") || strings.Contains(lower, "429")
}

func webSearchRateLimitError(provider string) error {
	if provider == "parallel" {
		return fmt.Errorf("Parallel web search rate limit reached. Set PARALLEL_API_KEY for your own quota: https://parallel.ai")
	}
	return fmt.Errorf("Exa web search rate limit reached. Set EXA_API_KEY for your own quota: https://dashboard.exa.ai/api-keys")
}
