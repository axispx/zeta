package ai

import (
	"encoding/json"
	"strings"
	"testing"
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
