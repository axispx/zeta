// Package todo is a session-scoped checklist the model writes during multi-step work.
package todo

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Status is the lifecycle state of one checklist item.
type Status string

const (
	Pending    Status = "pending"
	InProgress Status = "in_progress"
	Completed  Status = "completed"
	Cancelled  Status = "cancelled"
)

const (
	maxItems        = 20
	maxSubjectRunes = 200
	// Soft: more than this many in_progress → warning note on Replace.
	inProgressWarnAt = 1
)

// Item is one checklist entry.
type Item struct {
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
	Status      Status `json:"status"`
}

// Store is the in-memory checklist. Persistence is the todo tool result in the
// session transcript; resume rehydrates via ParseArgs on the last successful call.
type Store struct {
	mu    sync.Mutex
	items []Item
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{}
}

// Snapshot returns a copy of the current items.
func (s *Store) Snapshot() []Item {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneItems(s.items)
}

// Replace fully replaces the list. items may be empty to clear.
// Returns a soft warning when more than one item is in_progress.
func (s *Store) Replace(items []Item) (warning string, err error) {
	if s == nil {
		return "", fmt.Errorf("todo store is nil")
	}
	norm, err := Normalize(items)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.items = norm
	s.mu.Unlock()
	return inProgressWarning(norm), nil
}

// Format returns model-facing full list text for the current items.
func (s *Store) Format() string {
	return Format(s.Snapshot())
}

// PromptBlock is a markdown checklist for developer-message injection.
// Empty string when there are no items.
func (s *Store) PromptBlock() string {
	return PromptBlock(s.Snapshot())
}

// ParseArgs decodes todo tool arguments JSON and normalizes items.
func ParseArgs(raw json.RawMessage) ([]Item, error) {
	var a struct {
		Items []Item `json:"items"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if a.Items == nil {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if _, ok := probe["items"]; !ok {
			return nil, fmt.Errorf("items is required")
		}
		a.Items = []Item{}
	}
	return Normalize(a.Items)
}

// Normalize validates and copies items for Replace / resume.
func Normalize(items []Item) ([]Item, error) {
	if len(items) > maxItems {
		return nil, fmt.Errorf("at most %d todo items", maxItems)
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]Item, 0, len(items))
	for i, raw := range items {
		it, err := normalizeItem(raw)
		if err != nil {
			return nil, fmt.Errorf("items[%d]: %w", i, err)
		}
		if _, ok := seen[it.ID]; ok {
			return nil, fmt.Errorf("duplicate id %q", it.ID)
		}
		seen[it.ID] = struct{}{}
		out = append(out, it)
	}
	return out, nil
}

// Format returns model-facing full list text.
func Format(items []Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Todos (%d):", len(items))
	for _, it := range items {
		fmt.Fprintf(&b, "\n[%s] %s: %s", it.Status, it.ID, it.Subject)
		if d := strings.TrimSpace(it.Description); d != "" {
			fmt.Fprintf(&b, " — %s", d)
		}
	}
	return b.String()
}

// PromptBlock is a markdown checklist for developer-message injection.
// Empty string when there are no items.
func PromptBlock(items []Item) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Session todos\n")
	for _, it := range items {
		b.WriteString(promptLine(it))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func promptLine(it Item) string {
	mark := "[ ]"
	switch it.Status {
	case InProgress:
		mark = "[~]"
	case Completed:
		mark = "[x]"
	case Cancelled:
		mark = "[!]"
	}
	line := "- " + mark + " **" + it.ID + "**: " + it.Subject
	if d := strings.TrimSpace(it.Description); d != "" {
		line += " — " + d
	}
	return line
}

func inProgressWarning(items []Item) string {
	n := 0
	for _, it := range items {
		if it.Status == InProgress {
			n++
		}
	}
	if n > inProgressWarnAt {
		return fmt.Sprintf("warning: %d items in_progress", n)
	}
	return ""
}

func normalizeItem(raw Item) (Item, error) {
	id := strings.TrimSpace(raw.ID)
	if id == "" {
		return Item{}, fmt.Errorf("id is required")
	}
	sub := strings.TrimSpace(raw.Subject)
	if sub == "" {
		return Item{}, fmt.Errorf("subject is required")
	}
	if len([]rune(sub)) > maxSubjectRunes {
		return Item{}, fmt.Errorf("subject exceeds %d characters", maxSubjectRunes)
	}
	st := raw.Status
	if st == "" {
		st = Pending
	} else {
		parsed, err := parseStatus(string(st))
		if err != nil {
			return Item{}, err
		}
		st = parsed
	}
	return Item{
		ID:          id,
		Subject:     sub,
		Description: strings.TrimSpace(raw.Description),
		Status:      st,
	}, nil
}

func parseStatus(s string) (Status, error) {
	switch Status(strings.TrimSpace(s)) {
	case Pending:
		return Pending, nil
	case InProgress:
		return InProgress, nil
	case Completed:
		return Completed, nil
	case Cancelled:
		return Cancelled, nil
	default:
		return "", fmt.Errorf("unknown status %q", s)
	}
}

func cloneItems(in []Item) []Item {
	if len(in) == 0 {
		return nil
	}
	out := make([]Item, len(in))
	copy(out, in)
	return out
}
