package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// AskUser is the tool name for interactive clarifying questions.
const AskUser = "ask_user"

// UI always appends a freeform "Other" row, so the model may send at most 4
// options (5 rows total with Other). Cap questions to keep the panel focused.
const (
	askUserMaxQuestions = 3
	askUserMinOptions   = 2
	askUserMaxOptions   = 4
)

// AskOption is one multiple-choice row.
type AskOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AskQuestion is one prompt shown to the user.
type AskQuestion struct {
	ID       string      `json:"id"`
	Header   string      `json:"header"`
	Question string      `json:"question"`
	Options  []AskOption `json:"options"`
}

// AskUserArgs is the model-facing tool payload.
type AskUserArgs struct {
	Questions []AskQuestion `json:"questions"`
}

// AskUserResponse is returned to the model as the tool result JSON.
// Single-select: each question id maps to one answer string (option label or Other text).
type AskUserResponse struct {
	Answers map[string]string `json:"answers"`
}

// ParseAskUserArgs decodes and validates ask_user arguments.
func ParseAskUserArgs(raw json.RawMessage) (AskUserArgs, error) {
	var args AskUserArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return AskUserArgs{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if err := normalizeAskUserArgs(&args); err != nil {
		return AskUserArgs{}, err
	}
	return args, nil
}

func normalizeAskUserArgs(args *AskUserArgs) error {
	if args == nil || len(args.Questions) == 0 {
		return fmt.Errorf("questions is required")
	}
	if len(args.Questions) > askUserMaxQuestions {
		return fmt.Errorf("at most %d questions", askUserMaxQuestions)
	}
	seen := make(map[string]bool, len(args.Questions))
	for i := range args.Questions {
		q := &args.Questions[i]
		q.ID = strings.TrimSpace(q.ID)
		q.Header = strings.TrimSpace(q.Header)
		q.Question = strings.TrimSpace(q.Question)
		if q.ID == "" {
			return fmt.Errorf("questions[%d].id is required", i)
		}
		if seen[q.ID] {
			return fmt.Errorf("duplicate question id %q", q.ID)
		}
		seen[q.ID] = true
		if q.Question == "" {
			return fmt.Errorf("questions[%d].question is required", i)
		}
		if q.Header == "" {
			q.Header = q.ID
		}
		opts := make([]AskOption, 0, len(q.Options))
		for _, o := range q.Options {
			o.Label = strings.TrimSpace(o.Label)
			o.Description = strings.TrimSpace(o.Description)
			if o.Label == "" {
				continue
			}
			opts = append(opts, o)
			if len(opts) == askUserMaxOptions {
				break
			}
		}
		if len(opts) < askUserMinOptions {
			return fmt.Errorf("questions[%d] needs %d–%d options", i, askUserMinOptions, askUserMaxOptions)
		}
		q.Options = opts
	}
	return nil
}

// FormatAskUserResponse serializes answers for the model transcript.
// Marshal failures are reported as an error string the model can recover from.
func FormatAskUserResponse(resp AskUserResponse) string {
	b, err := json.Marshal(resp)
	if err != nil {
		return "error: failed to encode ask_user answers: " + err.Error()
	}
	return string(b)
}

type askUserTool struct{}

func (askUserTool) Name() string { return AskUser }

// Interactive: harness injects answers; Run is never used in the happy path.
func (askUserTool) Interactive() bool { return true }

func (askUserTool) Description() string {
	return "Pause and ask the user a multiple-choice question in the terminal UI. " +
		"Use only for choices that change the plan, load-bearing assumptions, " +
		"or preferences the codebase cannot settle. " +
		"Each question needs 2–4 real options; put the preferred option first and " +
		"suffix its label with \"(Recommended)\". Do not include an Other row — the UI adds freeform. " +
		"One question is best; never more than three in one call."
}

func (askUserTool) Parameters() map[string]any {
	optionSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label": map[string]any{
				"type":        "string",
				"description": "Short choice text (a few words). Preferred option first; end that label with \"(Recommended)\".",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "What picking this option means or changes (one short sentence).",
			},
		},
		"required": []string{"label", "description"},
	}
	questionSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "Key for the answer map (snake_case, unique in this call).",
			},
			"header": map[string]any{
				"type":        "string",
				"description": "Compact panel title (about a dozen characters or fewer).",
			},
			"question": map[string]any{
				"type":        "string",
				"description": "The question shown above the options.",
			},
			"options": map[string]any{
				"type":        "array",
				"description": "2–4 mutually exclusive choices. Preferred first with \"(Recommended)\". Omit Other.",
				"items":       optionSchema,
			},
		},
		"required": []string{"id", "header", "question", "options"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"questions": map[string]any{
				"type":        "array",
				"description": "One question is best; hard max three.",
				"items":       questionSchema,
			},
		},
		"required": []string{"questions"},
	}
}

func (askUserTool) Summary(raw json.RawMessage) string {
	args, err := ParseAskUserArgs(raw)
	if err != nil || len(args.Questions) == 0 {
		return AskUser
	}
	q := args.Questions[0]
	if h := strings.TrimSpace(q.Header); h != "" {
		if len(args.Questions) == 1 {
			return "ask " + h
		}
		return fmt.Sprintf("ask %s (+%d)", h, len(args.Questions)-1)
	}
	return AskUser
}

// Run is not used when the harness answers interactively; the agent injects the
// user response. Direct calls return an error so misuse is obvious.
func (askUserTool) Run(ctx context.Context, root string, raw json.RawMessage) (string, error) {
	if _, err := ParseAskUserArgs(raw); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s requires an interactive harness response", AskUser)
}
