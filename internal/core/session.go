package core

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

// Session is one conversation's harness state. It holds no UI state and no
// bubbletea import, so a TUI, a daemon, or a test can all drive the same
// Session.
type Session struct {
	WS            workspace.Context
	Cfg           config.Config
	Client        *ai.Client
	Usage         Usage
	Mode          prompt.Mode
	Rules         *permission.Rules
	Grants        *permission.Session
	Todos         *todo.Store
	History       []ai.Message
	Log           *session.Session
	ContextTokens int64
	ContextMsgs   int
	Compacting    bool
	AuthRetrying  bool
	AuthRetried   bool
	TitlePending  bool
	Streamed      bool
	Effects       bool
}

// RefreshWorkspace re-reads the volatile workspace fields at a turn boundary:
// the git branch, which the agent changes itself by checking out. AGENTS.md is
// deliberately not reloaded — it heads the request prefix providers cache, so
// reloading it every turn would invalidate the transcript whenever the file
// changed, including when the agent edits it. That snapshot moves only at a
// session boundary; see ReloadAgents.
//
// Branch is safe to refresh per turn because it feeds the trailing environment
// block (prompt.Environment, appended by RequestMsgs after the history), not
// the prefix. Only the last message of the request moves, so system, mode, and
// the durable history stay byte-identical and keep their cache hit.
func (s *Session) RefreshWorkspace() {
	s.WS.RefreshBranch()
}

// ReloadAgents re-reads AGENTS.md for the project-instructions half of the
// system prompt. Call only at a session boundary — /clear, /resume, compaction
// — never per turn. See workspace.Context.ReloadAgents for why: the file heads
// the cached request prefix, so reloading it mid-session would invalidate the
// transcript on every turn the agent touched it.
func (s *Session) ReloadAgents() {
	s.WS.ReloadAgents()
}

// RequestMsgs builds the next model request from this session's state.
func (s *Session) RequestMsgs() []ai.Message {
	return RequestMsgs(s.WS, s.Mode, s.History, s.Todos)
}

// Tools returns the tool set for the session's mode.
func (s *Session) Tools() []tools.Tool {
	return ToolsForMode(s.Mode, s.Todos)
}

// Gate reports whether the harness must decide before a tool runs, over this
// session's live rules and grants.
func (s *Session) Gate() func(name string, args json.RawMessage) bool {
	return Gate(s.Rules, s.Grants, s.WS.Abs)
}

// Persist appends one durable record to the on-disk log. No-op when the
// session has no log yet.
func (s *Session) Persist(rec session.Record) error {
	if s.Log == nil {
		return nil
	}
	return s.Log.Append(rec)
}

// CommitUserPrompt accepts a user turn into the session: it appends the turn to
// the API history and the durable log. Returns the persist error, if any.
func (s *Session) CommitUserPrompt(text string, imgs []image.Ref) error {
	s.History = append(s.History, ai.Message{Role: ai.RoleUser, Text: text, Images: imgs})
	return s.Persist(session.Record{Role: session.RoleUser, Text: text, Images: imgs})
}

// CommitAssistant records a completed assistant turn: it appends the message to
// the API history, banks the provider's token accounting, and persists the
// durable record. The measurement is taken against the history as it now stands
// — the provider billed this request for that history, and the completion
// tokens stand in for the assistant message they became, which was just
// appended — so the auto-compact budget check can use it instead of a chars/4
// guess. Returns the persist error, if any.
//
// Usage is attributed to the model that answered, not the currently active one:
// a /model switch mid-turn must not relabel the previous model's spend.
func (s *Session) CommitAssistant(m ai.Message, usage ai.Usage) error {
	s.History = append(s.History, m)
	if n := usage.ContextTokens(); n > 0 {
		s.ContextTokens = n
		s.ContextMsgs = len(s.History)
	}
	s.Usage.Add(s.Cfg.ModelName(), usage)

	rec := RecordFromAPI(m)
	rec.FramePlan = s.Mode == prompt.ModePlan
	// Persisted next to the turn it belongs to, so /usage totals survive
	// /resume. Nil when the provider reported nothing.
	rec.Usage = UsageOrNil(usage)
	if rec.Usage != nil {
		rec.Model = s.Cfg.ModelName()
	}
	return s.Persist(rec)
}

// CommitTool records a finished tool call: the result joins the API history and
// the durable record carries the row fields the transcript needs. Returns the
// persist error, if any.
func (s *Session) CommitTool(m ai.Message, label, name string, denied bool) error {
	s.History = append(s.History, m)
	return s.Persist(ToolRecord(m, label, name, denied))
}

// PolicyDenyReason is the reason recorded when a policy rule rejects a call.
const PolicyDenyReason = "denied by permission policy"

// AutoReply returns the reply the harness sends for a call it settles without
// the user, if any. WaitAutoDeny is the only such case today.
func AutoReply(kind WaitKind) (agent.Reply, bool) {
	if kind == WaitAutoDeny {
		return agent.DenyToolReason(PolicyDenyReason), true
	}
	return agent.Reply{}, false
}

// Run starts one turn's tool loop over this session's state. Composed here so
// every client runs the same wiring — this session's tools, root, gate, and
// request assembly — with d answering gated calls.
func (s *Session) Run(ctx context.Context, client *ai.Client, d agent.Decider) <-chan agent.Event {
	cfg := agent.Config{
		Client:  client,
		Tools:   s.Tools(),
		Root:    s.WS.Abs,
		Decider: d,
		Gate:    s.Gate(),
	}
	return cfg.Run(ctx, s.RequestMsgs())
}

// Persisted reports whether the session has been written to disk.
func (s *Session) Persisted() bool {
	return s.Log != nil && s.Log.Persisted()
}

// ResetContext drops the last provider measurement. Call whenever the request
// prefix changes (model, mode, or session switch): the measurement described a
// request this session will no longer send.
func (s *Session) ResetContext() {
	s.ContextTokens = 0
	s.ContextMsgs = 0
}

// Busy reports whether the session will not accept new input. turnActive is
// the caller's own turn, which the session does not track.
func (s *Session) Busy(turnActive bool) bool {
	return turnActive || s.Compacting || s.AuthRetrying
}

// Exclusive reports whether the in-flight job freezes the composer. An OAuth
// recover is busy but still accepts queued input.
func (s *Session) Exclusive() bool { return s.Compacting }

// BeginTurn resets the per-turn progress facts.
func (s *Session) BeginTurn() { s.Streamed, s.Effects = false, false }

// MarkStreamed records visible output from the current turn.
func (s *Session) MarkStreamed() { s.Streamed = true }

// MarkEffects records that a tool ran, or began running, this turn.
func (s *Session) MarkEffects() { s.Effects = true }

// CanReplay reports whether the current turn may be re-run. Output or a tool
// effect makes a replay unsafe: it would repeat the message, or worse, the
// side effect.
func (s *Session) CanReplay() bool { return !s.Streamed && !s.Effects }

// WantsTitle reports whether this session should ask for a generated name: it
// has a log, no name yet, and no request already in flight.
func (s *Session) WantsTitle() bool {
	return s.Log != nil && !s.TitlePending && s.Log.Name == ""
}

// GenerateTitle asks the client to name the session from prompt. The caller
// owns TitlePending and persists the result with ApplyTitle.
func (s *Session) GenerateTitle(ctx context.Context, client *ai.Client, prompt string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if client == nil || prompt == "" {
		return "", nil
	}
	return client.SessionTitle(ctx, prompt)
}

// ApplyTitle persists a generated session name. Blank names are a no-op.
func (s *Session) ApplyTitle(name string) error {
	if s.Log == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	return s.Log.SetName(name)
}

// CompactConfig builds the compaction thresholds from this session's prompt
// overhead and the caller's model context window.
func (s *Session) CompactConfig(cfg config.Config) compact.Config {
	overhead := compact.Estimate([]ai.Message{
		{Role: ai.RoleSystem, Text: prompt.System(s.WS)},
		{Role: ai.RoleDeveloper, Text: s.Mode.Instructions()},
		{Role: ai.RoleDeveloper, Text: prompt.Environment(s.WS)},
	})
	return compact.Config{
		ContextWindow: cfg.ContextWindow(),
		Overhead:      overhead + compact.DefaultToolsOverhead,
		// The provider's own count for the last request in this session, when
		// we have one, so the budget check is exact instead of chars/4.
		Measured:     int(s.ContextTokens),
		MeasuredMsgs: s.ContextMsgs,
	}
}

// CompactPrefix is the request head the summarizer must reuse. Same system
// prompt and tool definitions as a live turn, so the summarizer's request
// prefix is byte-identical to the conversation's and the provider serves the
// history it is summarizing from cache instead of charging full uncached input
// for it. A model or mode change between the turn and the compaction would
// forfeit that, which is why compaction snapshots this at the turn boundary.
func (s *Session) CompactPrefix() compact.Prefix {
	return compact.Prefix{
		Messages: RequestPrefix(s.WS, s.Mode),
		Tools:    tools.Defs(ToolsForMode(s.Mode, s.Todos)),
	}
}

// ShouldAutoCompact reports whether the next turn should compact first. client
// must be non-nil — it runs the summarizer.
func (s *Session) ShouldAutoCompact(client *ai.Client, cfg config.Config) bool {
	if client == nil || len(s.History) == 0 {
		return false
	}
	c := s.CompactConfig(cfg)
	if c.ContextWindow <= 0 {
		return false
	}
	return compact.Needed(s.History, c)
}
