package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestReasoningFromRaw(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"content only", `{"content":"hi"}`, ""},
		{"no reasoning key", `{"content":"hi","role":"assistant"}`, ""},
		{"reasoning_content", `{"reasoning_content":"think"}`, "think"},
		{"reasoning", `{"reasoning":"ponder"}`, "ponder"},
		{"prefers reasoning_content", `{"reasoning_content":"a","reasoning":"b"}`, "a"},
		{"invalid", `{`, ""},
		{"nullish", `{"reasoning_content":null}`, ""},
		// fast-path: substring "reasoning" without a real field still unmarshals empty
		{"false positive substring", `{"note":"reasoning about x"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reasoningFromRaw(tt.raw); got != tt.want {
				t.Fatalf("reasoningFromRaw(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestClassifyErr(t *testing.T) {
	got := classifyErr(&openai.Error{StatusCode: http.StatusUnauthorized})
	if !errors.Is(got, ErrAuth) {
		t.Fatalf("401: got %v", got)
	}
	if got.Error() == ErrAuth.Error() {
		t.Fatal("401 must wrap provider detail, not replace it")
	}
	// 403 (e.g. entitlement) is not an auth-refresh case.
	if got := classifyErr(&openai.Error{StatusCode: http.StatusForbidden}); errors.Is(got, ErrAuth) {
		t.Fatal("403 must not classify as ErrAuth")
	}
	plain := errors.New("boom")
	if got := classifyErr(plain); got != plain {
		t.Fatalf("plain error: got %v", got)
	}
	// Wrapped SDK errors still classify.
	wrapped := fmt.Errorf("stream: %w", &openai.Error{StatusCode: http.StatusUnauthorized})
	if got := classifyErr(wrapped); !errors.Is(got, ErrAuth) {
		t.Fatalf("wrapped 401: got %v", got)
	}
}

func TestToAPIMessagesMultimodal(t *testing.T) {
	dataURL := "data:image/png;base64,iVBORw0KGgo="
	msgs, err := toAPIMessages([]Message{
		{Role: RoleUser, Text: "look", Images: []Image{{URL: dataURL, MIME: "image/png", Name: "a.png"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].OfUser == nil {
		t.Fatalf("msgs=%#v", msgs)
	}
	parts := msgs[0].OfUser.Content.OfArrayOfContentParts
	if len(parts) != 2 {
		t.Fatalf("parts=%d", len(parts))
	}
	if parts[0].OfText == nil || parts[0].OfText.Text != "look" {
		t.Fatalf("text part=%#v", parts[0])
	}
	if parts[1].OfImageURL == nil {
		t.Fatal("expected image part")
	}
	if parts[1].OfImageURL.ImageURL.URL != dataURL {
		t.Fatalf("url=%q", parts[1].OfImageURL.ImageURL.URL)
	}

	_, err = toAPIMessages([]Message{
		{Role: RoleUser, Text: "x", Images: []Image{{URL: "", MIME: "image/png"}}},
	})
	if err == nil {
		t.Fatal("expected empty URL error")
	}
	_, err = toAPIMessages([]Message{
		{Role: RoleUser, Text: "x", Images: []Image{{URL: "/tmp/a.png", MIME: "image/png"}}},
	})
	if err == nil {
		t.Fatal("expected non-data URL error")
	}

	plain, err := toAPIMessages([]Message{{Role: RoleUser, Text: "hi"}})
	if err != nil || plain[0].OfUser == nil {
		t.Fatalf("plain: %#v err=%v", plain, err)
	}
	b, _ := json.Marshal(plain[0].OfUser.Content)
	if !strings.Contains(string(b), "hi") {
		t.Fatalf("content=%s", b)
	}
}

func TestCacheFromRawUsage(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantCached   int64
		wantWrite    int64
		wantReported bool
	}{
		{"empty", "", 0, 0, false},
		{"no cache fields", `{"prompt_tokens":100,"completion_tokens":5}`, 0, 0, false},
		{"invalid", `{`, 0, 0, false},
		{
			"openai details",
			`{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":80}}`,
			80, 0, true,
		},
		{
			"openai cache write",
			`{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":12}}`,
			80, 12, true,
		},
		{
			"deepseek top level",
			`{"prompt_tokens":18234,"prompt_cache_hit_tokens":16000,"prompt_cache_miss_tokens":2234}`,
			16000, 0, true,
		},
		{
			"anthropic gateway",
			`{"prompt_tokens":100,"cache_read_input_tokens":90,"cache_creation_input_tokens":5}`,
			90, 5, true,
		},
		{
			// A reported zero is a real cache miss, not missing accounting.
			"reported zero",
			`{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":0}}`,
			0, 0, true,
		},
		{
			// OpenAI spelling wins over the DeepSeek one when both appear.
			"details beat extras",
			`{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":80},"prompt_cache_hit_tokens":1}`,
			80, 0, true,
		},
		{
			// A write-only report still counts as cache accounting.
			"write only",
			`{"prompt_tokens":100,"cache_creation_input_tokens":5}`,
			0, 5, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cached, write, reported := cacheFromRawUsage(tt.raw)
			if cached != tt.wantCached || write != tt.wantWrite || reported != tt.wantReported {
				t.Fatalf("cacheFromRawUsage(%q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.raw, cached, write, reported, tt.wantCached, tt.wantWrite, tt.wantReported)
			}
		})
	}
}

func TestUsageCachedPercent(t *testing.T) {
	tests := []struct {
		name    string
		usage   Usage
		wantPct int
		wantOK  bool
	}{
		{"unreported hides", Usage{PromptTokens: 100}, 0, false},
		{"no prompt tokens", Usage{CacheReported: true}, 0, false},
		{"cold cache", Usage{PromptTokens: 100, CacheReported: true}, 0, true},
		{"full hit", Usage{PromptTokens: 100, CachedTokens: 100, CacheReported: true}, 100, true},
		{"typical hit", Usage{PromptTokens: 1000, CachedTokens: 970, CacheReported: true}, 97, true},
		{"truncates down", Usage{PromptTokens: 3, CachedTokens: 2, CacheReported: true}, 66, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct, ok := tt.usage.CachedPercent()
			if pct != tt.wantPct || ok != tt.wantOK {
				t.Fatalf("CachedPercent() = (%d, %v), want (%d, %v)", pct, ok, tt.wantPct, tt.wantOK)
			}
		})
	}
}

func TestUsageFromAccDropsUntypedUsage(t *testing.T) {
	// The accumulator cannot carry provider-specific cache fields, so they must
	// arrive via the separately captured raw usage JSON.
	raw := `{"prompt_tokens":18234,"completion_tokens":412,"total_tokens":18646,` +
		`"prompt_cache_hit_tokens":16000,"prompt_cache_miss_tokens":2234}`
	var chunk openai.ChatCompletionChunk
	if err := json.Unmarshal([]byte(`{"id":"c","object":"chat.completion.chunk",`+
		`"created":1,"model":"m","choices":[],"usage":`+raw+`}`), &chunk); err != nil {
		t.Fatal(err)
	}
	var acc openai.ChatCompletionAccumulator
	acc.AddChunk(chunk)

	got := usageFromAcc(acc, chunk.Usage.RawJSON())
	if got.PromptTokens != 18234 || got.CachedTokens != 16000 || !got.CacheReported {
		t.Fatalf("usage = %+v", got)
	}
	// Without the raw capture the metric is unreported, not 0%.
	blind := usageFromAcc(acc, "")
	if blind.CacheReported {
		t.Fatalf("absent raw usage must not report cache accounting: %+v", blind)
	}
}

func TestCleanTitle(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`"Auth Middleware Fix"`, "Auth Middleware Fix"},
		{"Title: Fix the flaky tests\n", "Fix the flaky tests"},
		{"  session: rename helper  ", "Rename helper"},
		{"# Ask Mode Prompt Critique & Suggestions", "Ask Mode Prompt Critique Suggestions"},
		{"Ask Mode Prompt Critique & Suggestions", "Ask Mode Prompt Critique Suggestions"},
		{"\n\nask mode prompt\nextra junk", "Ask mode prompt"},
		{
			"This is a very long session title that should be clipped for the picker UI",
			"This is a very long session title that should be",
		},
		{"flaky auth tests", "Flaky auth tests"},
		{"AI streaming bug", "AI streaming bug"},
	}
	for _, tt := range tests {
		if got := cleanTitle(tt.in); got != tt.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
