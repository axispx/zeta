package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAskUserArgsOK(t *testing.T) {
	raw := mustRaw(t, map[string]any{
		"questions": []any{
			map[string]any{
				"id":       "approach",
				"header":   "Approach",
				"question": "How should we structure this?",
				"options": []any{
					map[string]any{"label": "Simple (Recommended)", "description": "Minimal change."},
					map[string]any{"label": "Full rewrite", "description": "Bigger blast radius."},
				},
			},
		},
	})
	args, err := ParseAskUserArgs(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(args.Questions) != 1 || args.Questions[0].ID != "approach" {
		t.Fatalf("%+v", args)
	}
	if len(args.Questions[0].Options) != 2 {
		t.Fatalf("options: %+v", args.Questions[0].Options)
	}
}

func TestParseAskUserArgsRejectsEmptyOptions(t *testing.T) {
	raw := mustRaw(t, map[string]any{
		"questions": []any{
			map[string]any{
				"id": "q", "header": "H", "question": "Q?",
				"options": []any{map[string]any{"label": "only", "description": "one"}},
			},
		},
	})
	if _, err := ParseAskUserArgs(raw); err == nil {
		t.Fatal("expected error for single option")
	}
}

func TestParseAskUserArgsCapsOptions(t *testing.T) {
	opts := make([]any, 0, 6)
	for i := 0; i < 6; i++ {
		opts = append(opts, map[string]any{
			"label":       "opt",
			"description": "d",
		})
	}
	// make labels unique enough
	for i := range opts {
		opts[i].(map[string]any)["label"] = "opt" + string(rune('A'+i))
	}
	raw := mustRaw(t, map[string]any{
		"questions": []any{
			map[string]any{
				"id": "q", "header": "H", "question": "Q?",
				"options": opts,
			},
		},
	})
	args, err := ParseAskUserArgs(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(args.Questions[0].Options); n != askUserMaxOptions {
		t.Fatalf("got %d options, want %d", n, askUserMaxOptions)
	}
}

func TestParseAskUserArgsDuplicateID(t *testing.T) {
	raw := mustRaw(t, map[string]any{
		"questions": []any{
			map[string]any{
				"id": "q", "header": "H", "question": "Q1?",
				"options": []any{
					map[string]any{"label": "a", "description": "a"},
					map[string]any{"label": "b", "description": "b"},
				},
			},
			map[string]any{
				"id": "q", "header": "H", "question": "Q2?",
				"options": []any{
					map[string]any{"label": "a", "description": "a"},
					map[string]any{"label": "b", "description": "b"},
				},
			},
		},
	})
	if _, err := ParseAskUserArgs(raw); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err=%v", err)
	}
}

func TestFormatAskUserResponse(t *testing.T) {
	s := FormatAskUserResponse(AskUserResponse{
		Answers: map[string]string{
			"approach": "Simple (Recommended)",
		},
	})
	var got AskUserResponse
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatal(err)
	}
	if got.Answers["approach"] != "Simple (Recommended)" {
		t.Fatalf("%s", s)
	}
}

func TestAskUserSummary(t *testing.T) {
	raw := mustRaw(t, map[string]any{
		"questions": []any{
			map[string]any{
				"id": "q", "header": "Scope", "question": "Q?",
				"options": []any{
					map[string]any{"label": "a", "description": "a"},
					map[string]any{"label": "b", "description": "b"},
				},
			},
		},
	})
	if s := (askUserTool{}).Summary(raw); s != "ask Scope" {
		t.Fatalf("summary=%q", s)
	}
}
