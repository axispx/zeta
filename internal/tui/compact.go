package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/tools"
)

const (
	statusCompacting     = "Compacting"
	compactDividerText   = "Context compacted"
	compactNothingText   = "Nothing to compact"
	compactNoClientText  = "no provider configured"
	compactAutoFailText  = "Context compaction failed; continuing with full history"
	compactCancelledText = "Compaction cancelled"
)

// compactKind distinguishes manual /compact from auto-before-turn.
type compactKind int

const (
	compactManual compactKind = iota
	compactAuto
)

// compactDoneMsg is the result of a compact run (manual /compact or auto).
type compactDoneMsg struct {
	result      compact.Result
	err         error
	kind        compactKind
	titlePrompt string // first-user-prompt title seed when kind == compactAuto
}

// exclusiveJob freezes the composer while compact or self-update runs.
// Auth recover is busy but still accepts queue input — not exclusive.
func (m *Model) exclusiveJob() bool {
	return m.compacting || m.updating
}

// busy reports whether a turn, exclusive job, or auth recover is in flight.
func (m *Model) busy() bool {
	return m.turn != nil || m.exclusiveJob() || m.authRetrying
}

// compactConfig builds thresholds from the active model and prompt overhead.
func (m *Model) compactConfig() compact.Config {
	overhead := compact.Estimate([]ai.Message{
		{Role: ai.RoleSystem, Text: prompt.System(m.ws)},
		{Role: ai.RoleDeveloper, Text: m.mode.Instructions()},
		{Role: ai.RoleDeveloper, Text: prompt.Environment(m.ws)},
	})
	return compact.Config{
		ContextWindow: m.cfg.ContextWindow(),
		Overhead:      overhead + compact.DefaultToolsOverhead,
		// The provider's own count for the last request in this session, when
		// we have one, so the budget check is exact instead of chars/4.
		Measured:     int(m.contextTokens),
		MeasuredMsgs: m.contextMsgs,
	}
}

// compactPrefix is the request head the summarizer must reuse. Same system
// prompt and tool definitions as a live turn, so the summarizer's request
// prefix is byte-identical to the conversation's and the provider serves the
// history it is summarizing from cache instead of charging full uncached input
// for it. A model or mode change between the turn and the compaction would
// forfeit that, which is why compaction snapshots this at the turn boundary.
func (m *Model) compactPrefix() compact.Prefix {
	return compact.Prefix{
		Messages: requestPrefix(m.ws, m.mode),
		Tools:    tools.Defs(toolsForMode(m.mode, m.todos)),
	}
}

// shouldAutoCompact reports whether the next turn should compact first.
func (m *Model) shouldAutoCompact() bool {
	if m.client == nil || len(m.history) == 0 {
		return false
	}
	cfg := m.compactConfig()
	if cfg.ContextWindow <= 0 {
		return false
	}
	return compact.Needed(m.history, cfg)
}

// startCompact runs a manual /compact (forced). Context window is optional.
func (m *Model) startCompact() tea.Cmd {
	if m.busy() {
		return nil
	}
	if m.client == nil {
		m.noteSystem(compactNoClientText)
		return nil
	}
	if len(m.history) == 0 {
		m.noteSystem(compactNothingText)
		return nil
	}
	if err := m.ensureFreshClient(); err != nil {
		m.noteError(err.Error())
		return nil
	}
	// Match submit: overhead estimate uses current system prompt.
	m.refreshWorkspace()
	return m.runCompact(compactManual, "")
}

// runCompact starts an async compact. Auto kind continues into a turn when done.
func (m *Model) runCompact(kind compactKind, titlePrompt string) tea.Cmd {
	hist := append([]ai.Message(nil), m.history...)
	cfg := m.compactConfig()
	cfg.Prefix = m.compactPrefix()
	client := m.client
	force := kind == compactManual

	ctx, cancel := context.WithCancel(context.Background())
	m.compactCancel = cancel
	m.compacting = true
	// Busy gap grows while compacting; shrink transcript now.
	m.layoutPreservingBottom()

	return tea.Batch(func() tea.Msg {
		var res compact.Result
		var err error
		if force {
			res, err = compact.RunForced(ctx, client, hist, cfg)
		} else {
			res, err = compact.RunIfNeeded(ctx, client, hist, cfg)
		}
		return compactDoneMsg{
			result:      res,
			err:         err,
			kind:        kind,
			titlePrompt: titlePrompt,
		}
	}, m.spinner.Tick)
}

// cancelCompact aborts an in-flight compact (Esc). The async cmd still returns
// compactDoneMsg with context.Canceled.
func (m *Model) cancelCompact() {
	if m.compactCancel != nil {
		m.compactCancel()
		m.compactCancel = nil
	}
}

func (m *Model) clearCompactState() {
	m.compacting = false
	if m.compactCancel != nil {
		m.compactCancel()
		m.compactCancel = nil
	}
}

func (m *Model) handleCompactDone(msg compactDoneMsg) tea.Cmd {
	m.clearCompactState()
	if m.quitting {
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
	case msg.err != nil && msg.kind == compactAuto && !errors.Is(msg.err, context.Canceled):
		// Don't block the user turn — continue with full history.
		m.noteSystem(compactAutoFailText)
	}

	if msg.kind == compactAuto {
		// Compact can take a while, and the agent may have checked out a
		// branch in the meantime. AGENTS.md stays the session snapshot.
		m.refreshWorkspace()
		return m.beginTurn(msg.titlePrompt)
	}
	return nil
}

// resetUsage clears the last-response context + cache accounting shown in the
// footer. Call whenever the request prefix changes (model/mode/session switch):
// that also discards any provider measurement, since it described a request the
// session will no longer send.
func (m *Model) resetUsage() {
	m.contextTokens = 0
	m.contextMsgs = 0
	m.cacheStats = cacheStats{}
}

func (m *Model) applyCompactResult(res compact.Result) {
	// Compaction is when project instructions are re-read, alongside /clear and
	// /resume. It is nearly free: the conversation layer is being rewritten
	// anyway, and an unchanged AGENTS.md leaves the system prompt layer intact.
	// Reload before the overhead estimate below so it reflects the new prompt.
	m.ws.ReloadAgents()
	m.history = res.History
	// The provider's measurement described the pre-compaction request, so it is
	// gone. Re-base on an estimate of the new history: the footer shows the
	// shrink immediately, and the next auto-compact check measures only what is
	// appended from here (see compact.usedTokens).
	m.contextTokens = int64(compact.Estimate(res.History) + m.compactConfig().Overhead)
	m.contextMsgs = len(m.history)
	// Compaction rewrites the prefix, so any previous hit rate is stale.
	m.cacheStats = cacheStats{}
	m.messages = append(m.messages, Message{Role: RoleSystem, Text: compactDividerText})
	m.persist(session.Record{
		Role: session.RoleCompact,
		Text: res.Summary,
		Tail: res.TailCount,
	})
	m.refreshTranscript()
}

func (m *Model) noteSystem(text string) {
	m.messages = append(m.messages, Message{Role: RoleSystem, Text: text})
	m.refreshTranscript()
}

func (m *Model) noteError(text string) {
	m.messages = append(m.messages, Message{Role: RoleError, Text: text})
	m.refreshTranscript()
}
