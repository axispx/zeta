package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
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
}

func TestDropLastUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s, err := New(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleUser, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleAgent, Text: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleUser, Text: "second"}); err != nil {
		t.Fatal(err)
	}

	dropped, err := s.DropLastUser()
	if err != nil || !dropped {
		t.Fatalf("dropped=%v err=%v", dropped, err)
	}

	_, recs, err := OpenID(proj, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Text != "first" || recs[1].Text != "ok" {
		t.Fatalf("recs=%#v", recs)
	}

	dropped, err = s.DropLastUser()
	if err != nil {
		t.Fatal(err)
	}
	if dropped {
		t.Fatal("must not drop a trailing agent turn")
	}
}

func TestDropLastUserUnpersistsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s, err := New(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleUser, Text: "only"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetName("Titled"); err != nil {
		t.Fatal(err)
	}

	dropped, err := s.DropLastUser()
	if err != nil || !dropped {
		t.Fatalf("dropped=%v err=%v", dropped, err)
	}
	if s.Persisted() {
		t.Fatal("should be unpersisted")
	}
	if _, err := os.Stat(s.Path); !os.IsNotExist(err) {
		t.Fatalf("jsonl should be gone: %v", err)
	}
	entries, err := List(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("index=%#v", entries)
	}

	// Same session id can persist again after a later send.
	if err := s.Append(Record{Role: RoleUser, Text: "retry"}); err != nil {
		t.Fatal(err)
	}
	if !s.Persisted() {
		t.Fatal("re-append should persist")
	}
	_, recs, err := OpenID(proj, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Text != "retry" {
		t.Fatalf("recs=%#v", recs)
	}
}

func TestLoadSkipsUnknownEventTypes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s, err := New(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleUser, Text: "before"}); err != nil {
		t.Fatal(err)
	}
	// Inject a deleted side-channel event (old todos snapshots) between messages.
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"todos","items":[{"id":"1","subject":"legacy","status":"pending"}]}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleAgent, Text: "after"}); err != nil {
		t.Fatal(err)
	}

	s2, recs, err := OpenID(proj, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s2.ID != s.ID {
		t.Fatalf("id = %q", s2.ID)
	}
	if len(recs) != 2 || recs[0].Text != "before" || recs[1].Text != "after" {
		t.Fatalf("recs = %#v", recs)
	}
}

// Usage rides on the assistant record it belongs to, so a resumed session can
// total spend without a second event type.
func TestUsageRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s, _, err := Open(proj)
	if err != nil {
		t.Fatal(err)
	}
	usage := &ai.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		TotalTokens:      1200,
		CachedTokens:     900,
		CacheReported:    true,
		CacheWriteTokens: 50,
	}
	if err := s.Append(Record{Role: RoleAgent, Text: "hi", Usage: usage}); err != nil {
		t.Fatal(err)
	}
	// Turns with no accounting stay bare: no empty usage object on the wire.
	if err := s.Append(Record{Role: RoleAgent, Text: "no usage"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"prompt_tokens":1000`) {
		t.Fatalf("usage not written inline:\n%s", raw)
	}
	if n := strings.Count(string(raw), `"usage"`); n != 1 {
		t.Fatalf("usage key on %d events, want 1:\n%s", n, raw)
	}

	_, recs, err := OpenID(proj, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("recs = %#v", recs)
	}
	if recs[0].Usage == nil || *recs[0].Usage != *usage {
		t.Fatalf("usage = %+v, want %+v", recs[0].Usage, usage)
	}
	if recs[1].Usage != nil {
		t.Fatalf("unreported turn should stay nil: %+v", recs[1].Usage)
	}
}

// Usage and its model attribution round-trip together, so a resumed session can
// total spend and still say which model produced it.
func TestUsageModelRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	proj := filepath.Join(t.TempDir(), "proj")

	s, _, err := Open(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{
		Role:  RoleAgent,
		Text:  "a",
		Model: "GPT-4",
		Usage: &ai.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Role: RoleAgent, Text: "b", Model: "v4-flash", Usage: &ai.Usage{PromptTokens: 200, CompletionTokens: 20}}); err != nil {
		t.Fatal(err)
	}
	// No usage: the caller sets no model either, so the record carries neither.
	if err := s.Append(Record{Role: RoleAgent, Text: "c"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"model"`); n != 2 {
		t.Fatalf("model key on %d events, want 2:\n%s", n, raw)
	}

	_, recs, err := OpenID(proj, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("recs = %#v", recs)
	}
	if recs[0].Model != "GPT-4" || recs[1].Model != "v4-flash" {
		t.Fatalf("models = %q/%q", recs[0].Model, recs[1].Model)
	}
}
