package harness

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/tools"
)

func TestAutoReplyDeniesWaitAutoDeny(t *testing.T) {
	r, ok := AutoReply(WaitAutoDeny)
	if !ok {
		t.Fatal("WaitAutoDeny should settle without the user")
	}
	if r.Kind != ReplyDeny || r.Reason != PolicyDenyReason {
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
	{
		s := &Session{Todos: store}
		got := s.CompactPrefix()
		wantMsgs := RequestPrefix(s.WS)
		if !reflect.DeepEqual(got.Messages, wantMsgs) {
			t.Fatalf("messages = %+v, want %+v", got.Messages, wantMsgs)
		}
		wantTools := tools.Defs(TurnTools(s.Todos))
		if len(got.Tools) != len(wantTools) {
			t.Fatalf("tools = %d, want %d", len(got.Tools), len(wantTools))
		}
		for i := range wantTools {
			if got.Tools[i].Name != wantTools[i].Name {
				t.Fatalf("tool %d = %q, want %q", i, got.Tools[i].Name, wantTools[i].Name)
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
	s := &Session{Log: log}
	usage := ai.Usage{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200, CachedTokens: 900, CacheReported: true}

	if err := s.CommitAssistant(ai.Message{Role: ai.RoleAssistant, Text: "hi"}, usage, nil); err != nil {
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
	s := &Session{Log: log}
	if err := s.CommitAssistant(ai.Message{Role: ai.RoleAssistant, Text: "hi"}, ai.Usage{}, nil); err != nil {
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

func codexSessionClient(model string) *ai.Client {
	return ai.New(config.Provider{
		BaseURL: codex.BaseURL,
		OAuth:   &config.OAuthCredential{AccessToken: "t", AccountID: "a"},
		Models:  map[string]config.ModelDef{model: {ContextWindow: 30_000}},
	}, model)
}

func windowedCfg(base string, window int) config.Config {
	return config.Config{
		Active: "p/m",
		Providers: map[string]config.Provider{
			"p": {BaseURL: base, APIKey: "k", Models: map[string]config.ModelDef{"m": {ContextWindow: window}}},
		},
	}
}

// A native provider compacts the whole history, so being over budget is the
// only condition: it does not wait for a head the summarizer could free.
func TestShouldAutoCompactNative(t *testing.T) {
	long := []ai.Message{{Role: ai.RoleUser, Text: "go"}}
	for i := 0; i < 30; i++ {
		id := string(rune('a' + i))
		long = append(long,
			ai.Message{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: id, Name: "read", Arguments: `{}`}}},
			ai.Message{Role: ai.RoleTool, ToolCallID: id, Text: strings.Repeat("word ", 1000)},
		)
	}
	cfg := windowedCfg(codex.BaseURL, 30_000)
	client := codexSessionClient("m")

	s := &Session{History: long, Cfg: cfg}
	if !s.ShouldAutoCompact(client, cfg) {
		t.Fatal("an over-budget history must compact")
	}
	s = &Session{History: long[:5], Cfg: cfg}
	if s.ShouldAutoCompact(client, cfg) {
		t.Fatal("a history within budget must not compact")
	}
	// Only one message: nothing a summarizer could split, but still over budget.
	one := []ai.Message{{Role: ai.RoleUser, Text: strings.Repeat("word ", 30_000)}}
	s = &Session{History: one, Cfg: cfg}
	if s.ShouldAutoCompact(ai.New(config.Provider{BaseURL: "https://api.example.com/v1", APIKey: "k"}, "m"), cfg) {
		t.Fatal("the summarizer has nothing to free in a single message")
	}
	if !s.ShouldAutoCompact(client, cfg) {
		t.Fatal("a native provider is asked even when the summarizer could not split")
	}
}

// A checkpoint is opaque and bound to its model. When the model changes the
// history is rebuilt from the log, with that checkpoint skipped.
func TestReconcileCompactionRebuildsFromLog(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	cwd := t.TempDir()
	log, err := session.New(cwd)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []session.Record{
		{Role: session.RoleUser, Text: "first"},
		{Role: session.RoleAgent, Text: "ok"},
		{Role: session.RoleUser, Text: "second"},
		{Role: session.RoleCompact, Native: "ENC", NativeModel: "gpt-5.5"},
	} {
		if err := log.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	checkpointed := []ai.Message{
		{Role: ai.RoleUser, Text: "first"},
		{Role: ai.RoleUser, Text: "second"},
		{Role: ai.RoleCompaction, Compaction: &ai.Compaction{Content: "ENC", Model: "gpt-5.5"}},
	}

	// The same model keeps its checkpoint.
	s := &Session{Log: log, History: append([]ai.Message(nil), checkpointed...), Client: codexSessionClient("gpt-5.5")}
	s.ReconcileCompaction()
	if len(s.History) != 3 || s.History[2].Role != ai.RoleCompaction {
		t.Fatalf("own checkpoint was dropped: %d messages", len(s.History))
	}

	// A model that cannot read it gets the covered turns back, raw.
	s = &Session{Log: log, History: append([]ai.Message(nil), checkpointed...),
		Client: ai.New(config.Provider{BaseURL: "https://api.example.com/v1", APIKey: "k"}, "m")}
	s.ReconcileCompaction()
	if len(s.History) != 3 || s.History[1].Role != ai.RoleAssistant || s.History[1].Text != "ok" {
		t.Fatalf("history = %d messages, want the raw turns back", len(s.History))
	}
	for _, m := range s.History {
		if m.Role == ai.RoleCompaction {
			t.Fatal("a checkpoint survived a model that cannot read it")
		}
	}

	// With no log to rebuild from, the checkpoint is dropped and the rest kept.
	s = &Session{History: append([]ai.Message(nil), checkpointed...),
		Client: ai.New(config.Provider{BaseURL: "https://api.example.com/v1", APIKey: "k"}, "m")}
	s.ReconcileCompaction()
	if len(s.History) != 2 {
		t.Fatalf("history = %d messages, want the 2 user messages", len(s.History))
	}
}
