package todo

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
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
	items, w, ok := ParseFormat(got)
	if !ok || w != "" || len(items) != 3 || items[1].Description != "JSONL" {
		t.Fatalf("ParseFormat=%v warn=%q ok=%v", items, w, ok)
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
	out := s.Format() + "\n" + warn
	items, w, ok := ParseFormat(out)
	if !ok || len(items) != 2 || w != warn {
		t.Fatalf("items=%v w=%q ok=%v", items, w, ok)
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

func TestOnChange(t *testing.T) {
	s := NewStore()
	var got []Item
	s.SetOnChange(func(items []Item) error {
		got = items
		return nil
	})
	if _, err := s.Replace([]Item{{ID: "1", Subject: "a", Status: Pending}}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("onChange=%+v", got)
	}
	// Seed does not fire.
	got = nil
	s.Seed([]Item{{ID: "x", Subject: "from session", Status: Completed}})
	if got != nil {
		t.Fatal("Seed should not fire OnChange")
	}
	if snap := s.Snapshot(); len(snap) != 1 || snap[0].ID != "x" {
		t.Fatalf("%+v", snap)
	}
}

func TestOnChangeRollback(t *testing.T) {
	s := NewStore()
	if _, err := s.Replace([]Item{{ID: "1", Subject: "keep"}}); err != nil {
		t.Fatal(err)
	}
	s.SetOnChange(func([]Item) error { return errors.New("disk full") })
	if _, err := s.Replace([]Item{{ID: "2", Subject: "new"}}); err == nil {
		t.Fatal("want error")
	}
	if snap := s.Snapshot(); len(snap) != 1 || snap[0].ID != "1" {
		t.Fatalf("rolled back? %+v", snap)
	}
}

func TestConcurrentSnapshotReplace(t *testing.T) {
	s := NewStore()
	if _, err := s.Replace([]Item{{ID: "1", Subject: "a", Status: Pending}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var fails atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.Snapshot()
			_ = s.Format()
			_ = s.PromptBlock()
		}()
		go func() {
			defer wg.Done()
			if _, err := s.Replace([]Item{{ID: "1", Subject: "a", Status: InProgress}}); err != nil {
				fails.Add(1)
			}
		}()
	}
	wg.Wait()
	if fails.Load() != 0 {
		t.Fatalf("replace fails=%d", fails.Load())
	}
	if len(s.Snapshot()) != 1 {
		t.Fatalf("snap=%+v", s.Snapshot())
	}
}

func TestGlyph(t *testing.T) {
	if Glyph(Pending) != "○" || Glyph(InProgress) != "◐" || Glyph(Completed) != "●" || Glyph(Cancelled) != "✗" {
		t.Fatal("glyphs")
	}
}

func TestSeed(t *testing.T) {
	s := NewStore()
	s.Seed([]Item{{ID: "x", Subject: "from session", Status: Completed}})
	if got := s.Snapshot(); len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("%+v", got)
	}
	s.Seed(nil)
	if len(s.Snapshot()) != 0 {
		t.Fatal("clear")
	}
}
