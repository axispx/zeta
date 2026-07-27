// Package todo is a session-scoped checklist the model writes during multi-step work.
package todo

import (
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
	maxItems         = 20
	maxSubjectRunes  = 200
	inProgressWarnAt = 1 // soft: more than this many in_progress → warning note
)

// Item is one checklist entry.
type Item struct {
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
	Status      Status `json:"status"`
}

// Store is a mutex-guarded todo list.
// Optional OnChange runs after a successful Replace (not Seed); used to persist.
type Store struct {
	mu       sync.Mutex
	items    []Item
	onChange func([]Item) error
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{}
}

// SetOnChange registers a hook invoked with a snapshot after Replace.
// A nil fn clears the hook. Seed does not fire the hook.
func (s *Store) SetOnChange(fn func([]Item) error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
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
// OnChange failures roll back the in-memory list and return the error.
func (s *Store) Replace(items []Item) (warning string, err error) {
	if s == nil {
		return "", fmt.Errorf("todo store is nil")
	}
	norm, err := validateReplace(items)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	prev := s.items
	s.items = norm
	fn := s.onChange
	s.mu.Unlock()

	if fn != nil {
		if err := fn(cloneItems(norm)); err != nil {
			s.mu.Lock()
			s.items = prev
			s.mu.Unlock()
			return "", err
		}
	}
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

// Seed loads items from session restore. Invalid payloads clear the store.
// Does not invoke OnChange (already persisted).
func (s *Store) Seed(items []Item) {
	if s == nil {
		return
	}
	norm, err := validateReplace(items)
	if err != nil {
		norm = nil
	}
	s.mu.Lock()
	s.items = norm
	s.mu.Unlock()
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

// Glyph is the transcript status mark for st.
func Glyph(st Status) string {
	switch st {
	case Pending:
		return "○"
	case InProgress:
		return "◐"
	case Completed:
		return "●"
	case Cancelled:
		return "✗"
	default:
		return "·"
	}
}

// ParseFormat parses Format output (and optional trailing "warning:" lines).
// ok is false when s is not Format-shaped.
func ParseFormat(s string) (items []Item, warning string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, "", false
	}
	lines := strings.Split(s, "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "Todos (") {
		return nil, "", false
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "warning:") {
			warning = line
			continue
		}
		it, err := parseFormatLine(line)
		if err != nil {
			return nil, "", false
		}
		items = append(items, it)
	}
	return items, warning, true
}

func parseFormatLine(line string) (Item, error) {
	if !strings.HasPrefix(line, "[") {
		return Item{}, fmt.Errorf("missing status")
	}
	end := strings.IndexByte(line, ']')
	if end < 0 {
		return Item{}, fmt.Errorf("bad status")
	}
	st, err := parseStatus(line[1:end])
	if err != nil {
		return Item{}, err
	}
	rest := strings.TrimSpace(line[end+1:])
	id, rest, cut := strings.Cut(rest, ": ")
	if !cut || strings.TrimSpace(id) == "" {
		return Item{}, fmt.Errorf("missing id")
	}
	subject, desc, _ := strings.Cut(rest, " — ")
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return Item{}, fmt.Errorf("missing subject")
	}
	return Item{
		ID:          strings.TrimSpace(id),
		Subject:     subject,
		Description: strings.TrimSpace(desc),
		Status:      st,
	}, nil
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

func validateReplace(items []Item) ([]Item, error) {
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

func normalizeItem(raw Item) (Item, error) {
	id := strings.TrimSpace(raw.ID)
	if id == "" {
		return Item{}, fmt.Errorf("id is required")
	}
	sub := strings.TrimSpace(raw.Subject)
	if sub == "" {
		return Item{}, fmt.Errorf("subject is required")
	}
	if err := checkSubject(sub); err != nil {
		return Item{}, err
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

func checkSubject(sub string) error {
	if len([]rune(sub)) > maxSubjectRunes {
		return fmt.Errorf("subject exceeds %d characters", maxSubjectRunes)
	}
	return nil
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
