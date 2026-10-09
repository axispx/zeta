package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/harness"
	"github.com/axispx/zeta/internal/session"
)

const (
	statusCompacting     = "Compacting"
	compactDividerText   = "Context compacted"
	compactNothingText   = "Nothing to compact"
	compactNoClientText  = "no provider configured"
	compactAutoFailText  = "Context compaction failed; continuing with full history"
	compactCancelledText = "Compaction cancelled"
)

// compactKind distinguishes manual /compact, auto-before-turn, and a turn
// handed back mid-way because its next request would not fit.
type compactKind int

const (
	compactManual compactKind = iota
	compactAuto
	compactResume
)

// compactDoneMsg is the result of a compact run (manual /compact or auto).
type compactDoneMsg struct {
	result      compact.Result
	err         error
	kind        compactKind
	titlePrompt string // first-user-prompt title seed when kind == compactAuto
}

// exclusiveJob freezes the composer while a compact run is in flight.
// Auth recover is busy but still accepts queue input — not exclusive.
func (m *Model) exclusiveJob() bool {
	return m.session.Exclusive()
}

// busy reports whether a turn, exclusive job, or auth recover is in flight.
func (m *Model) busy() bool {
	return m.session.Busy(m.turn.current != nil)
}

// startCompact runs a manual /compact (forced). Context window is optional.
func (m *Model) startCompact() tea.Cmd {
	if m.busy() {
		return nil
	}
	if m.session.Client == nil {
		m.noteSystem(compactNoClientText)
		return nil
	}
	if len(m.session.History) == 0 {
		m.noteSystem(compactNothingText)
		return nil
	}
	if err := m.session.EnsureFreshClient(context.Background()); err != nil {
		m.noteError(err.Error())
		return nil
	}
	// Match submit: overhead estimate uses current system prompt.
	m.session.RefreshWorkspace()
	return m.runCompact(compactManual, "")
}

// runCompact starts an async compact. Auto kind continues into a turn when done.
func (m *Model) runCompact(kind compactKind, titlePrompt string) tea.Cmd {
	hist := append([]ai.Message(nil), m.session.History...)
	cfg := m.session.CompactConfig(m.session.Cfg)
	cfg.Prefix = m.session.CompactPrefix()
	client := m.session.Client
	force := kind == compactManual

	ctx, cancel := context.WithCancel(context.Background())
	m.turn.compactCancel = cancel
	m.session.Compacting = true
	// Busy gap grows while compacting; shrink transcript now.
	m.layoutPreservingBottom()

	return tea.Batch(func() tea.Msg {
		res, err := runCompaction(ctx, client, hist, cfg, force)
		return compactDoneMsg{
			result:      res,
			err:         err,
			kind:        kind,
			titlePrompt: titlePrompt,
		}
	}, m.spinner.Tick)
}

// handleTurnCompact takes a turn the loop handed back at a tool boundary
// because its next request would not fit, compacts the history it has recorded
// so far, and resumes it. The loop has already ended, so everything it did is in
// history and the transcript.
func (m *Model) handleTurnCompact() tea.Cmd {
	if m.turn.current == nil {
		return nil
	}
	m.turn.carried = m.turn.current.steers.drain()
	m.finishTurn()
	if err := m.session.EnsureFreshClient(context.Background()); err != nil {
		m.turn.restoreCarried(&m.queue)
		m.noteError(err.Error())
		return nil
	}
	m.session.RefreshWorkspace()
	return m.runCompact(compactResume, "")
}

// resumeTurn starts the agent loop again on the compacted history, for the turn
// handleTurnCompact interrupted. It is not beginTurn: the turn's streamed and
// effect marks stay, so a credential failure still cannot replay it.
func (m *Model) resumeTurn() tea.Cmd {
	if m.session.Client == nil {
		return nil
	}
	m.session.RefreshWorkspace()
	cmds := []tea.Cmd{m.turn.start(m.session.Client, &m.session), m.spinner.Tick}
	for _, p := range m.turn.carried {
		m.turn.current.steers.push(p)
	}
	m.turn.carried = nil
	m.afterQueueChange()
	m.layoutPreservingBottom()
	return tea.Batch(cmds...)
}

// restoreCarried returns carried steers to the front of the queue (they were
// sent before anything queued).
func (t *turn) restoreCarried(q *queue) {
	if len(t.carried) > 0 {
		q.prompts = append(t.carried, q.prompts...)
		t.carried = nil
	}
}

// runCompaction compacts hist: with the provider's own compaction when it has
// one, which keeps what the model understood of the work rather than a note
// about it, and otherwise (or when that fails) by summarizing.
func runCompaction(ctx context.Context, client *ai.Client, hist []ai.Message, cfg compact.Config, force bool) (compact.Result, error) {
	if client.NativeCompaction() {
		res, err := compact.RunNative(ctx, client, hist, cfg, force)
		if err == nil || ctx.Err() != nil {
			return res, err
		}
		// A failed native request leaves the history untouched; the summarizer
		// reads the same history, so it is a safe second try.
	}
	if force {
		return compact.RunForced(ctx, client, hist, cfg)
	}
	return compact.RunIfNeeded(ctx, client, hist, cfg)
}

// cancelCompact aborts an in-flight compact (Esc). The async cmd still returns
// compactDoneMsg with context.Canceled.
func (m *Model) cancelCompact() {
	m.turn.abortCompact()
}

func (m *Model) clearCompactState() {
	m.session.Compacting = false
	m.turn.abortCompact()
}

func (m *Model) handleCompactDone(msg compactDoneMsg) tea.Cmd {
	m.clearCompactState()
	if m.exit.quitting {
		return nil
	}

	// Notify on outcome; auto path always continues into the user turn below.
	switch {
	case msg.err == nil && msg.result.Compacted:
		m.applyCompactResult(msg.result)
	case msg.err == nil && msg.kind == compactManual:
		m.noteSystem(compactNothingText)
	case errors.Is(msg.err, context.Canceled) && msg.kind == compactManual:
		m.noteSystem(compactCancelledText)
	case msg.err != nil && msg.kind == compactManual:
		m.noteError(msg.err.Error())
	case msg.err != nil && msg.kind != compactManual && !errors.Is(msg.err, context.Canceled):
		// Don't block the user turn — continue with full history.
		m.noteSystem(compactAutoFailText)
	}

	if msg.kind == compactResume {
		if errors.Is(msg.err, context.Canceled) {
			// Esc during the compaction cancels the turn it interrupted.
			m.turn.restoreCarried(&m.queue)
			m.restoreQueuedIntoComposer()
			m.noteSystem(turnCancelledText)
			return nil
		}
		// A compaction that freed nothing will free nothing next round either:
		// run the rest of the turn without asking again.
		m.session.HoldCompact = !msg.result.Compacted
		return m.resumeTurn()
	}
	if msg.kind == compactAuto {
		// Compact can take a while, and the agent may have checked out a
		// branch in the meantime. AGENTS.md stays the session snapshot.
		m.session.RefreshWorkspace()
		return m.beginTurn(msg.titlePrompt)
	}
	return nil
}

func (m *Model) applyCompactResult(res compact.Result) {
	// Compaction is when project instructions are re-read, alongside /clear and
	// /resume. It is nearly free: the conversation layer is being rewritten
	// anyway, and an unchanged AGENTS.md leaves the system prompt layer intact.
	// Reload before the overhead estimate below so it reflects the new prompt.
	m.session.ReloadAgents()
	m.session.History = res.History
	// The provider's measurement described the pre-compaction request, so it is
	// gone. Re-base on an estimate of the new history: the footer shows the
	// shrink immediately, and the next auto-compact check measures only what is
	// appended from here (see compact.usedTokens).
	m.session.ContextTokens = int64(compact.Estimate(res.History) + m.session.CompactConfig(m.session.Cfg).Overhead)
	m.session.ContextMsgs = len(m.session.History)
	m.transcript.messages = append(m.transcript.messages, Message{Role: RoleSystem, Text: compactDividerText})
	rec := session.Record{
		Role: session.RoleCompact,
		Text: res.Summary,
		Tail: res.TailCount,
	}
	if res.Native != nil {
		// The checkpoint is what rebuilds this history on /resume (see
		// compact.NativeHistory); Text and Tail do not apply. The request's
		// usage rides on the record so /usage still totals a resumed session.
		rec = session.Record{
			Role:        session.RoleCompact,
			Native:      res.Native.Content,
			NativeModel: res.Native.Model,
		}
	}
	if u := harness.UsageOrNil(res.Usage); u != nil {
		rec.Usage, rec.Model = u, m.session.Cfg.ModelName()
		m.session.Usage.Add(rec.Model, res.Usage)
	}
	m.persist(rec)
	m.refreshTranscript()
}

func (m *Model) noteSystem(text string) {
	m.transcript.messages = append(m.transcript.messages, Message{Role: RoleSystem, Text: text})
	m.refreshTranscript()
}

func (m *Model) noteError(text string) {
	m.transcript.messages = append(m.transcript.messages, Message{Role: RoleError, Text: text})
	m.refreshTranscript()
}
