package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/oauth"
)

// oauthTestCfg is a model selection backed by an OAuth provider (no API key).
// The base URL is unroutable: tests never complete a real model call.
func oauthTestCfg() config.Config {
	return config.Config{
		Active: "xai/grok",
		Providers: map[string]config.Provider{
			"xai": {
				BaseURL: "http://127.0.0.1:1",
				OAuth: &config.OAuthCredential{
					AccessToken:  "at",
					RefreshToken: "rt",
					ExpiresAt:    time.Now().UnixMilli() + 60*60*1000,
				},
				Models: map[string]config.ModelDef{"grok": {ContextWindow: 128000}},
			},
		},
	}
}

// fakeTurn is a turnSession whose cancel is a no-op and whose event channel is
// closed, so a retry restart is the only live behavior. The caller sets the
// session's Streamed/Effects flags to model a turn that has already progressed.
func fakeTurn(id int) *turnSession {
	return &turnSession{
		id:         id,
		cancel:     func() {},
		ch:         closedAgentEvents(),
		activeTool: -1,
	}
}

// oauthTokenServer serves a rotating refresh response at oauth.XaiTokenURL.
func oauthTokenServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prev := oauth.XaiTokenURL
	oauth.XaiTokenURL = srv.URL
	t.Cleanup(func() { oauth.XaiTokenURL = prev })
	return srv
}

// execAuthRetryCmd runs the cmd returned by startAuthRetry (Batch of recover + spinner).
func execAuthRetryCmd(t *testing.T, cmd tea.Cmd) authRetryResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	msg := cmd()
	// tea.Batch may return a []Cmd wrapper; unwrap until we get the result msg.
	for {
		switch v := msg.(type) {
		case authRetryResultMsg:
			return v
		case tea.BatchMsg:
			for _, c := range v {
				if c == nil {
					continue
				}
				inner := c()
				if got, ok := inner.(authRetryResultMsg); ok {
					return got
				}
			}
			t.Fatalf("batch had no authRetryResultMsg: %#v", v)
		default:
			// Single cmd that itself returns the msg.
			if got, ok := msg.(authRetryResultMsg); ok {
				return got
			}
			t.Fatalf("unexpected msg %T", msg)
		}
	}
}

func TestTurnErrAuthRetriesOnce(t *testing.T) {
	isolateZetaHome(t)
	oauthTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-at",
			"refresh_token": "new-rt",
			"expires_in":    3600,
			"token_type":    "bearer",
		})
	})

	m := testModel()
	m.session.Cfg = oauthTestCfg()
	m.session.ApplyClient()
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "hello"}}
	m.turn.nextID = 5
	m.turn.current = fakeTurn(5)

	cmd := m.handleTurnErr(ai.ErrAuth)
	if cmd == nil {
		t.Fatal("expected retry cmd")
	}
	if !m.session.AuthRetried {
		t.Fatal("authRetried not set")
	}
	// Turn is finished while the async recover runs; busy stays true.
	if m.turn.current != nil {
		t.Fatal("turn should be finished during recover")
	}
	if !m.session.AuthRetrying || !m.busy() {
		t.Fatal("should be busy while recovering")
	}

	got := execAuthRetryCmd(t, cmd)
	if got.err != nil {
		t.Fatalf("recover err: %v", got.err)
	}
	follow := m.handleAuthRetryResult(got)
	if follow == nil {
		t.Fatal("expected beginTurn cmd")
	}
	if m.turn.current == nil || m.turn.current.id != 6 {
		t.Fatalf("turn not restarted: %+v", m.turn.current)
	}
	if got := m.session.Cfg.Providers["xai"].OAuth.AccessToken; got != "new-at" {
		t.Fatalf("access token = %q", got)
	}
	if got := m.session.Cfg.Providers["xai"].OAuth.RefreshToken; got != "new-rt" {
		t.Fatalf("refresh token = %q", got)
	}
	if len(m.session.History) != 1 || m.session.History[0].Text != "hello" {
		t.Fatalf("history changed: %+v", m.session.History)
	}
	m.finishTurn()
}

func TestTurnErrAuthNoRetryAfterProgress(t *testing.T) {
	isolateZetaHome(t)
	m := testModel()
	m.session.Cfg = oauthTestCfg()
	m.session.ApplyClient()
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "hello"}}
	m.turn.nextID = 5
	m.turn.current = fakeTurn(5)
	m.session.Streamed = true

	cmd := m.handleTurnErr(ai.ErrAuth)
	if cmd != nil {
		t.Fatal("no retry after progress")
	}
	if m.session.AuthRetried {
		t.Fatal("authRetried must stay false")
	}
	if m.turn.current != nil {
		t.Fatal("turn should be finished")
	}
	if n := len(m.transcript.messages); n == 0 || m.transcript.messages[n-1].Role != RoleError {
		t.Fatalf("messages=%+v", m.transcript.messages)
	}
}

func TestTurnErrAuthNoRetryAfterEffects(t *testing.T) {
	isolateZetaHome(t)
	m := testModel()
	m.session.Cfg = oauthTestCfg()
	m.session.ApplyClient()
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "hello"}}
	m.turn.nextID = 5
	m.turn.current = fakeTurn(5)
	// A tool ran: replaying would repeat its side effect, even though nothing
	// was streamed to the transcript.
	m.session.Effects = true

	cmd := m.handleTurnErr(ai.ErrAuth)
	if cmd != nil {
		t.Fatal("no retry once a tool has run")
	}
	if m.session.AuthRetried {
		t.Fatal("authRetried must stay false")
	}
	if m.turn.current != nil {
		t.Fatal("turn should be finished")
	}
}

func TestTurnErrAuthNoRetryApiKey(t *testing.T) {
	m := testModelWithClient() // API-key provider
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "hello"}}
	m.turn.nextID = 5
	m.turn.current = fakeTurn(5)

	cmd := m.handleTurnErr(ai.ErrAuth)
	if cmd != nil {
		t.Fatal("no retry for API-key provider")
	}
	if m.session.AuthRetried {
		t.Fatal("authRetried must stay false")
	}
	if m.turn.current != nil {
		t.Fatal("turn should be finished")
	}
	if n := len(m.transcript.messages); n == 0 || m.transcript.messages[n-1].Role != RoleError {
		t.Fatalf("messages=%+v", m.transcript.messages)
	}
}

func TestAuthRetryingBlocksSubmitAndQueueDeliver(t *testing.T) {
	isolateZetaHome(t)
	m := testModel()
	m.session.Cfg = oauthTestCfg()
	m.session.ApplyClient()
	m.session.AuthRetrying = true
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "hello"}}

	// Direct submit must not start a turn under the recover wait.
	if cmd := m.submit("next", nil); cmd != nil {
		t.Fatal("submit must no-op while authRetrying")
	}
	if m.turn.current != nil {
		t.Fatal("submit must not create a turn while authRetrying")
	}
	if len(m.session.History) != 1 {
		t.Fatalf("history mutated: %+v", m.session.History)
	}

	// Queue deliver (empty Enter / focus Enter) must not interrupt recover.
	m.queue.prompts = []queuedPrompt{newQueuedPrompt(1, "queued", nil)}
	if cmd := m.deliverQueued(1); cmd != nil {
		t.Fatal("deliverQueued must no-op while authRetrying")
	}
	if len(m.queue.prompts) != 1 {
		t.Fatalf("queue item dropped: %+v", m.queue.prompts)
	}

	// Composer text during recover is enqueued, not submitted.
	m.composer.textarea.SetValue("follow up")
	if cmd := m.submitInput(); cmd != nil {
		t.Fatalf("submitInput cmd = %T", cmd)
	}
	if len(m.queue.prompts) != 2 {
		t.Fatalf("expected enqueue, queue=%+v", m.queue.prompts)
	}
	if m.turn.current != nil || !m.session.AuthRetrying {
		t.Fatal("recover wait must stay busy without a turn")
	}
}

func TestTurnErrAuthRefreshRejectedSurfacesReauth(t *testing.T) {
	isolateZetaHome(t)
	oauthTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	})

	m := testModel()
	m.session.Cfg = oauthTestCfg()
	m.session.ApplyClient()
	m.session.History = []ai.Message{{Role: ai.RoleUser, Text: "hello"}}
	m.turn.nextID = 5
	m.turn.current = fakeTurn(5)

	cmd := m.handleTurnErr(ai.ErrAuth)
	if cmd == nil {
		t.Fatal("expected recover cmd")
	}
	if !m.session.AuthRetried {
		t.Fatal("authRetried should be set after one attempt")
	}

	got := execAuthRetryCmd(t, cmd)
	follow := m.handleAuthRetryResult(got)
	if follow != nil {
		t.Fatal("no retry when refresh is rejected")
	}
	if m.turn.current != nil {
		t.Fatal("turn should stay finished")
	}
	if n := len(m.transcript.messages); n == 0 || m.transcript.messages[n-1].Role != RoleError {
		t.Fatalf("messages=%+v", m.transcript.messages)
	}
	text := m.transcript.messages[len(m.transcript.messages)-1].Text
	if text != config.ErrReauthRequired.Error() {
		t.Fatalf("error = %q", text)
	}
	if !m.session.Cfg.Providers["xai"].OAuth.RefreshFailed {
		t.Fatal("RefreshFailed not installed in memory")
	}

	// A later 401 short-circuits instead of replaying the doomed refresh.
	m.session.AuthRetried = false
	m.turn.current = fakeTurn(6)
	cmd = m.handleTurnErr(ai.ErrAuth)
	if cmd != nil {
		t.Fatal("second auth error must surface, not retry")
	}
	if m.session.AuthRetried {
		t.Fatal("authRetried must stay false when canRetryOAuth is false")
	}
}
