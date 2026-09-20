package core

import (
	"context"
	"reflect"
	"testing"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/tools"
)

func TestAutoReplyDeniesWaitAutoDeny(t *testing.T) {
	r, ok := AutoReply(WaitAutoDeny)
	if !ok {
		t.Fatal("WaitAutoDeny should settle without the user")
	}
	if r.Kind != agent.ReplyDeny || r.Reason != PolicyDenyReason {
		t.Fatalf("reply=%+v", r)
	}
}

func TestAutoReplyLeavesOtherKindsToTheUser(t *testing.T) {
	for _, k := range []WaitKind{WaitNone, WaitPermission, WaitInteractive} {
		if r, ok := AutoReply(k); ok {
			t.Fatalf("kind=%v should not auto-reply: %+v", k, r)
		}
	}
}

// TestCompactPrefixMatchesTurnPrefix pins the prompt-cache invariant: the
// summarizer's request head must stay byte-identical to a live turn's, or the
// provider re-reads the whole transcript it is being asked to summarize.
func TestCompactPrefixMatchesTurnPrefix(t *testing.T) {
	store := todo.NewStore()
	if _, err := store.Replace([]todo.Item{{ID: "1", Subject: "A", Status: todo.Pending}}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []prompt.Mode{prompt.ModeBuild, prompt.ModeAsk, prompt.ModePlan} {
		s := &Session{Mode: mode, Todos: store}
		got := s.CompactPrefix()
		wantMsgs := RequestPrefix(s.WS, s.Mode)
		if !reflect.DeepEqual(got.Messages, wantMsgs) {
			t.Fatalf("mode %v: messages = %+v, want %+v", mode, got.Messages, wantMsgs)
		}
		wantTools := tools.Defs(ToolsForMode(s.Mode, s.Todos))
		if len(got.Tools) != len(wantTools) {
			t.Fatalf("mode %v: tools = %d, want %d", mode, len(got.Tools), len(wantTools))
		}
		for i := range wantTools {
			if got.Tools[i].Name != wantTools[i].Name {
				t.Fatalf("mode %v: tool %d = %q, want %q", mode, i, got.Tools[i].Name, wantTools[i].Name)
			}
		}
	}
}

func TestBusyCountsSessionJobsAndCallerTurn(t *testing.T) {
	s := &Session{}
	if s.Busy(false) {
		t.Fatal("idle session should not be busy")
	}
	if !s.Busy(true) {
		t.Fatal("a caller turn should make it busy")
	}
	s.Compacting = true
	if !s.Busy(false) {
		t.Fatal("compacting should be busy")
	}
	s.Compacting = false
	s.AuthRetrying = true
	if !s.Busy(false) {
		t.Fatal("auth recover should be busy")
	}
}

func TestExclusiveIsCompactingOnly(t *testing.T) {
	s := &Session{}
	if s.Exclusive() {
		t.Fatal("idle is not exclusive")
	}
	// Auth recover is busy but still accepts queued input.
	s.AuthRetrying = true
	if s.Exclusive() {
		t.Fatal("auth recover must not freeze the composer")
	}
	s.Compacting = true
	if !s.Exclusive() {
		t.Fatal("compacting should be exclusive")
	}
}

func TestCanReplayUntilOutputOrEffects(t *testing.T) {
	for _, tc := range []struct {
		name     string
		streamed bool
		effects  bool
	}{
		{"untouched", false, false},
		{"streamed", true, false},
		{"effects", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{}
			if tc.streamed {
				s.MarkStreamed()
			}
			if tc.effects {
				s.MarkEffects()
			}
			want := !tc.streamed && !tc.effects
			if got := s.CanReplay(); got != want {
				t.Fatalf("CanReplay = %v, want %v", got, want)
			}
		})
	}
}

// TestBeginTurnClearsProgressButKeepsAuthRetried pins the retry-loop guard: a
// turn is retried at most once, so restarting one must not re-arm the retry.
func TestBeginTurnClearsProgressButKeepsAuthRetried(t *testing.T) {
	s := &Session{Streamed: true, Effects: true, AuthRetried: true}
	s.BeginTurn()
	if !s.CanReplay() {
		t.Fatal("BeginTurn should clear the replay gate")
	}
	if !s.AuthRetried {
		t.Fatal("BeginTurn must not re-arm the 401 retry")
	}
}

func TestWantsTitle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sess    *Session
		pending bool
		want    bool
	}{
		{"no log", &Session{}, false, false},
		{"untitled new session", &Session{Log: &session.Session{}}, false, true},
		{"already requested", &Session{Log: &session.Session{}}, true, false},
		{"already named", &Session{Log: &session.Session{Name: "Fix tests"}}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.sess.TitlePending = tc.pending
			if got := tc.sess.WantsTitle(); got != tc.want {
				t.Fatalf("WantsTitle = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyTitlePersistsAndIgnoresBlank(t *testing.T) {
	s := &Session{Log: &session.Session{}}
	if err := s.ApplyTitle("  Fix flaky tests  "); err != nil {
		t.Fatal(err)
	}
	if s.Log.Name != "Fix flaky tests" {
		t.Fatalf("name = %q", s.Log.Name)
	}
	if s.WantsTitle() {
		t.Fatal("a named session should not want a title")
	}
	// Blank and no-log cases are no-ops, not errors.
	if err := s.ApplyTitle("   "); err != nil {
		t.Fatal(err)
	}
	if err := (&Session{}).ApplyTitle("x"); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateTitleWithoutClientOrPrompt(t *testing.T) {
	s := &Session{Log: &session.Session{}}
	if name, err := s.GenerateTitle(context.Background(), nil, "hello"); err != nil || name != "" {
		t.Fatalf("nil client: name=%q err=%v", name, err)
	}
	if name, err := s.GenerateTitle(context.Background(), &ai.Client{}, "   "); err != nil || name != "" {
		t.Fatalf("blank prompt: name=%q err=%v", name, err)
	}
}

func TestCommitUserPromptAppendsHistoryAndLog(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	dir := t.TempDir()
	log, err := session.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Log: log}
	img := image.Ref{URL: "data:image/png;base64,AAAA", MIME: "image/png"}
	if err := s.CommitUserPrompt("hello", []image.Ref{img}); err != nil {
		t.Fatal(err)
	}
	if len(s.History) != 1 || s.History[0].Role != ai.RoleUser || s.History[0].Text != "hello" || len(s.History[0].Images) != 1 {
		t.Fatalf("history=%+v", s.History)
	}
	_, recs, err := session.OpenID(dir, log.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Role != session.RoleUser || recs[0].Text != "hello" || len(recs[0].Images) != 1 {
		t.Fatalf("recs=%+v", recs)
	}
}

func TestCommitAssistantAppendsBanksAndPersists(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	dir := t.TempDir()
	log, err := session.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Log: log, Mode: prompt.ModePlan}
	usage := ai.Usage{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200, CachedTokens: 900, CacheReported: true}

	if err := s.CommitAssistant(ai.Message{Role: ai.RoleAssistant, Text: "hi"}, usage); err != nil {
		t.Fatal(err)
	}
	if len(s.History) != 1 || s.History[0].Role != ai.RoleAssistant || s.History[0].Text != "hi" {
		t.Fatalf("history=%+v", s.History)
	}
	if s.ContextTokens != 1200 || s.ContextMsgs != 1 {
		t.Fatalf("measurement tokens=%d msgs=%d", s.ContextTokens, s.ContextMsgs)
	}
	if s.Usage.Responses != 1 || s.Usage.Total != 1200 {
		t.Fatalf("usage=%+v", s.Usage)
	}

	_, recs, err := session.OpenID(dir, log.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Role != session.RoleAgent || recs[0].Text != "hi" {
		t.Fatalf("recs=%+v", recs)
	}
	if recs[0].Usage == nil || *recs[0].Usage != usage {
		t.Fatalf("persisted usage=%+v", recs[0].Usage)
	}
	if recs[0].Model != s.Cfg.ModelName() {
		t.Fatalf("persisted model=%q", recs[0].Model)
	}
	if !recs[0].FramePlan {
		t.Fatal("plan-mode segment should persist FramePlan")
	}
}

// A provider that reports no token counts must not bank a measurement or write
// a Usage block, but the turn still lands in history and on disk.
func TestCommitAssistantWithoutUsage(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	dir := t.TempDir()
	log, err := session.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Log: log, Mode: prompt.ModeBuild}
	if err := s.CommitAssistant(ai.Message{Role: ai.RoleAssistant, Text: "hi"}, ai.Usage{}); err != nil {
		t.Fatal(err)
	}
	if s.ContextTokens != 0 || s.ContextMsgs != 0 {
		t.Fatalf("unreported usage must not bank: tokens=%d msgs=%d", s.ContextTokens, s.ContextMsgs)
	}
	if s.Usage.Responses != 0 || !s.Usage.Empty() {
		t.Fatalf("usage=%+v", s.Usage)
	}
	_, recs, err := session.OpenID(dir, log.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Usage != nil || recs[0].Model != "" {
		t.Fatalf("recs=%+v", recs)
	}
	if recs[0].FramePlan {
		t.Fatal("build-mode segment must not frame a plan")
	}
}

func TestCommitToolAppendsHistoryAndPersistsRow(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	dir := t.TempDir()
	log, err := session.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Log: log}
	msg := ai.Message{Role: ai.RoleTool, Text: "ok", ToolCallID: "c1"}
	if err := s.CommitTool(msg, "bash ls", tools.Bash, true); err != nil {
		t.Fatal(err)
	}
	if len(s.History) != 1 || s.History[0].ToolCallID != "c1" {
		t.Fatalf("history=%+v", s.History)
	}
	_, recs, err := session.OpenID(dir, log.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Role != session.RoleTool || recs[0].ToolCallID != "c1" {
		t.Fatalf("recs=%+v", recs)
	}
	if recs[0].Label != "bash ls" || recs[0].Tool != tools.Bash || !recs[0].Denied {
		t.Fatalf("row fields=%+v", recs[0])
	}
}
