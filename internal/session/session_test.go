package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/axispx/zeta/internal/todo"
)

func TestCwdKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/Users/alice/Developer/zeta", "Users-alice-Developer-zeta"},
		{"/", "default"},
		{"C:\\foo\\bar", "C-foo-bar"},
	}
	for _, tt := range tests {
		if got := CwdKey(tt.in); got != tt.want {
			t.Errorf("CwdKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOpenAppendResume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s, recs, err := Open(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("fresh session records = %d", len(recs))
	}
	if len(s.Todos()) != 0 {
		t.Fatalf("fresh todos = %#v", s.Todos())
	}

	// Empty sessions are not indexed.
	entries, err := List(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty session should not be indexed: %#v", entries)
	}

	if err := s.Append(Record{
		Role: RoleUser,
		Text: "fix the auth middleware timeout",
		Images: []ImageRef{
			{URL: "data:image/png;base64,AAAA", MIME: "image/png", Name: "shot.png"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetName("Auth Middleware Fix"); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleAgent, Text: "hello", FramePlan: true}); err != nil {
		t.Fatal(err)
	}

	s2, recs2, err := Open(proj)
	if err != nil {
		t.Fatal(err)
	}
	if s2.ID != s.ID {
		t.Fatalf("id = %q, want %q", s2.ID, s.ID)
	}
	if len(s2.Todos()) != 0 {
		t.Fatalf("todos without AppendTodos = %#v", s2.Todos())
	}
	name, err := s2.IndexedName()
	if err != nil {
		t.Fatal(err)
	}
	if name != "Auth Middleware Fix" {
		t.Fatalf("indexed name = %q", name)
	}
	if s2.Name != "Auth Middleware Fix" {
		t.Fatalf("hydrated name = %q", s2.Name)
	}
	if len(recs2) != 2 {
		t.Fatalf("recs = %#v", recs2)
	}
	if len(recs2[0].Images) != 1 || recs2[0].Images[0].URL != "data:image/png;base64,AAAA" || recs2[0].Images[0].MIME != "image/png" {
		t.Fatalf("images = %#v", recs2[0].Images)
	}
	if !recs2[1].FramePlan {
		t.Fatal("FramePlan not restored from JSONL")
	}

	// File is a single JSONL with typed events; no sidecar.
	f, err := os.Open(s2.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var types []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var evt map[string]any
		if err := json.Unmarshal(sc.Bytes(), &evt); err != nil {
			t.Fatal(err)
		}
		types = append(types, evt["type"].(string))
	}
	want := []string{"session", "message", "message"}
	if len(types) != len(want) {
		t.Fatalf("event types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("event types = %v, want %v", types, want)
		}
	}

	entries, err = List(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("index entries = %#v", entries)
	}
	if entries[0].ID != s.ID || entries[0].Name != "Auth Middleware Fix" {
		t.Fatalf("index entry = %#v", entries[0])
	}
	if entries[0].Updated == "" || entries[0].Created == "" {
		t.Fatalf("index timestamps missing: %#v", entries[0])
	}
}

func TestNewNotPersistedUntilAppend(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s, err := New(proj)
	if err != nil {
		t.Fatal(err)
	}
	if s.Persisted() {
		t.Fatal("new session should not be persisted yet")
	}
	if _, err := os.Stat(s.Path); !os.IsNotExist(err) {
		t.Fatalf("jsonl should not exist yet: %v", err)
	}
	entries, err := List(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("new empty session indexed: %#v", entries)
	}

	// SetName before first Append keeps the name in memory only.
	if err := s.SetName("Pending Title"); err != nil {
		t.Fatal(err)
	}
	if s.Name != "Pending Title" {
		t.Fatalf("in-memory name = %q", s.Name)
	}
	entries, err = List(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("SetName should not index empty session: %#v", entries)
	}

	if err := s.Append(Record{Role: RoleUser, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if !s.Persisted() {
		t.Fatal("session should be persisted after append")
	}
	if _, err := os.Stat(s.Path); err != nil {
		t.Fatalf("jsonl after append: %v", err)
	}
	entries, err = List(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != s.ID {
		t.Fatalf("after append = %#v", entries)
	}
	if entries[0].Name != "Pending Title" {
		t.Fatalf("name should persist on first append: %#v", entries[0])
	}
}

func TestListOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s1, err := New(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Append(Record{Role: RoleUser, Text: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s1.SetName("First"); err != nil {
		t.Fatal(err)
	}

	s2, err := New(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Append(Record{Role: RoleUser, Text: "two"}); err != nil {
		t.Fatal(err)
	}
	if err := s2.SetName("Second"); err != nil {
		t.Fatal(err)
	}

	entries, err := List(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d", len(entries))
	}
	if entries[0].Name != "Second" || entries[1].Name != "First" {
		t.Fatalf("order = %#v", entries)
	}
}

func TestOpenID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s1, err := New(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Append(Record{Role: RoleUser, Text: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s1.SetName("First"); err != nil {
		t.Fatal(err)
	}

	s2, err := New(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Append(Record{Role: RoleUser, Text: "two"}); err != nil {
		t.Fatal(err)
	}
	if err := s2.SetName("Second"); err != nil {
		t.Fatal(err)
	}

	got, recs, err := OpenID(proj, s1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != s1.ID {
		t.Fatalf("id = %q, want %q", got.ID, s1.ID)
	}
	if got.Name != "First" {
		t.Fatalf("hydrated name = %q", got.Name)
	}
	name, err := got.IndexedName()
	if err != nil {
		t.Fatal(err)
	}
	if name != "First" {
		t.Fatalf("name = %q", name)
	}
	if len(recs) != 1 || recs[0].Text != "one" {
		t.Fatalf("recs = %#v", recs)
	}
	if len(got.Todos()) != 0 {
		t.Fatalf("todos = %#v", got.Todos())
	}
}

func TestAppendTodosLastWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s, err := New(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleUser, Text: "start"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendTodos([]todo.Item{
		{ID: "1", Subject: "first", Status: todo.Pending},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleAgent, Text: "working"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendTodos([]todo.Item{
		{ID: "1", Subject: "first", Status: todo.Completed},
		{ID: "2", Subject: "second", Status: todo.InProgress},
	}); err != nil {
		t.Fatal(err)
	}
	if todos := s.Todos(); len(todos) != 2 || todos[0].Status != todo.Completed {
		t.Fatalf("live Todos = %#v", todos)
	}

	s2, recs, err := OpenID(proj, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s2.ID != s.ID {
		t.Fatalf("id = %q", s2.ID)
	}
	if len(recs) != 2 {
		t.Fatalf("recs = %#v", recs)
	}
	todos := s2.Todos()
	if len(todos) != 2 || todos[0].Status != todo.Completed || todos[1].ID != "2" {
		t.Fatalf("todos = %#v", todos)
	}

	// Clear snapshot round-trips.
	if err := s.AppendTodos(nil); err != nil {
		t.Fatal(err)
	}
	if len(s.Todos()) != 0 {
		t.Fatalf("live clear = %#v", s.Todos())
	}
	s3, _, err := OpenID(proj, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s3.Todos()) != 0 {
		t.Fatalf("cleared todos = %#v", s3.Todos())
	}
}
