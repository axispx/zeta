package todo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReplaceAndFormat(t *testing.T) {
	s := NewStore()
	warn, err := s.Replace([]Item{
		{ID: "1", Subject: "Wire todo store", Status: InProgress},
		{ID: "2", Subject: "Session persist", Description: "JSONL", Status: Pending},
		{ID: "3", Subject: "Package tests", Status: Completed},
	})
	if err != nil {
		t.Fatal(err)
	}
	if warn != "" {
		t.Fatalf("warn=%q", warn)
	}
	got := s.Format()
	want := "Todos (3):\n[in_progress] 1: Wire todo store\n[pending] 2: Session persist — JSONL\n[completed] 3: Package tests"
	if got != want {
		t.Fatalf("Format=\n%s\nwant\n%s", got, want)
	}
}

func TestReplaceClear(t *testing.T) {
	s := NewStore()
	if _, err := s.Replace([]Item{{ID: "a", Subject: "x"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Replace(nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Format(); got != "Todos (0):" {
		t.Fatalf("got %q", got)
	}
	if s.PromptBlock() != "" {
		t.Fatal("PromptBlock should be empty")
	}
}

func TestReplaceValidation(t *testing.T) {
	s := NewStore()
	if _, err := s.Replace([]Item{{ID: "", Subject: "x"}}); err == nil {
		t.Fatal("empty id")
	}
	if _, err := s.Replace([]Item{{ID: "1", Subject: "  "}}); err == nil {
		t.Fatal("empty subject")
	}
	if _, err := s.Replace([]Item{{ID: "1", Subject: "x", Status: "nope"}}); err == nil {
		t.Fatal("bad status")
	}
	if _, err := s.Replace([]Item{
		{ID: "1", Subject: "a"},
		{ID: "1", Subject: "b"},
	}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("dup: %v", err)
	}
	tooLong := strings.Repeat("x", maxSubjectRunes+1)
	if _, err := s.Replace([]Item{{ID: "1", Subject: tooLong}}); err == nil {
		t.Fatal("subject length")
	}
	items := make([]Item, maxItems+1)
	for i := range items {
		items[i].ID = strings.Repeat("i", i+1)
		items[i].Subject = "s"
	}
	if _, err := s.Replace(items); err == nil {
		t.Fatal("max items")
	}
}

func TestInProgressWarning(t *testing.T) {
	s := NewStore()
	warn, err := s.Replace([]Item{
		{ID: "1", Subject: "a", Status: InProgress},
		{ID: "2", Subject: "b", Status: InProgress},
	})
	if err != nil {
		t.Fatal(err)
	}
	if warn != "warning: 2 items in_progress" {
		t.Fatalf("warn=%q", warn)
	}
}

func TestPromptBlock(t *testing.T) {
	s := NewStore()
	if s.PromptBlock() != "" {
		t.Fatal("empty")
	}
	if _, err := s.Replace([]Item{
		{ID: "1", Subject: "A", Status: Pending},
		{ID: "2", Subject: "B", Description: "more", Status: InProgress},
		{ID: "3", Subject: "C", Status: Completed},
		{ID: "4", Subject: "D", Status: Cancelled},
	}); err != nil {
		t.Fatal(err)
	}
	got := s.PromptBlock()
	if !strings.HasPrefix(got, "# Session todos\n") {
		t.Fatalf("prefix: %q", got)
	}
	for _, line := range []string{
		"- [ ] **1**: A",
		"- [~] **2**: B — more",
		"- [x] **3**: C",
		"- [!] **4**: D",
	} {
		if !strings.Contains(got, line) {
			t.Fatalf("missing %q in\n%s", line, got)
		}
	}
}

func TestSnapshotCopy(t *testing.T) {
	s := NewStore()
	if _, err := s.Replace([]Item{{ID: "1", Subject: "a"}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	snap[0].Subject = "mutated"
	if s.Snapshot()[0].Subject != "a" {
		t.Fatal("snapshot not a copy")
	}
}

func TestReplaceClears(t *testing.T) {
	s := NewStore()
	if _, err := s.Replace([]Item{{ID: "x", Subject: "from session", Status: Completed}}); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot(); len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("%+v", got)
	}
	if _, err := s.Replace(nil); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot()) != 0 {
		t.Fatal("clear")
	}
}

func TestParseArgs(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"items": []map[string]any{
			{"id": "1", "subject": "A", "status": "pending"},
			{"id": "2", "subject": "B", "status": "in_progress"},
		},
	})
	items, err := ParseArgs(raw)
	if err != nil || len(items) != 2 || items[1].Status != InProgress {
		t.Fatalf("items=%+v err=%v", items, err)
	}

	empty, err := ParseArgs(json.RawMessage(`{"items":[]}`))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	if _, err := ParseArgs(json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing items")
	}
	if _, err := ParseArgs(json.RawMessage(`{"items":[{"id":"1","subject":"x","status":"nope"}]}`)); err == nil {
		t.Fatal("bad status")
	}
}
