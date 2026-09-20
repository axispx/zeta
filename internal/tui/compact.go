package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
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

// exclusiveJob freezes the composer while a compact run is in flight.
// Auth recover is busy but still accepts queue input — not exclusive.
func (m *Model) exclusiveJob() bool {
	return m.Exclusive()
}

// busy reports whether a turn, exclusive job, or auth recover is in flight.
func (m *Model) busy() bool {
	return m.Busy(m.turn != nil)
}

// startCompact runs a manual /compact (forced). Context window is optional.
func (m *Model) startCompact() tea.Cmd {
	if m.busy() {
		return nil
	}
	if m.Client == nil {
		m.noteSystem(compactNoClientText)
		return nil
	}
	if len(m.History) == 0 {
		m.noteSystem(compactNothingText)
		return nil
	}
	if err := m.EnsureFreshClient(context.Background()); err != nil {
		m.noteError(err.Error())
		return nil
	}
	// Match submit: overhead estimate uses current system prompt.
	m.RefreshWorkspace()
	return m.runCompact(compactManual, "")
}

// runCompact starts an async compact. Auto kind continues into a turn when done.
func (m *Model) runCompact(kind compactKind, titlePrompt string) tea.Cmd {
	hist := append([]ai.Message(nil), m.History...)
	cfg := m.CompactConfig(m.Cfg)
	cfg.Prefix = m.CompactPrefix()
	client := m.Client
	force := kind == compactManual

	ctx, cancel := context.WithCancel(context.Background())
	m.compactCancel = cancel
	m.Compacting = true
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
	m.Compacting = false
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
		m.RefreshWorkspace()
		return m.beginTurn(msg.titlePrompt)
	}
	return nil
}

func (m *Model) applyCompactResult(res compact.Result) {
	// Compaction is when project instructions are re-read, alongside /clear and
	// /resume. It is nearly free: the conversation layer is being rewritten
	// anyway, and an unchanged AGENTS.md leaves the system prompt layer intact.
	// Reload before the overhead estimate below so it reflects the new prompt.
	m.ReloadAgents()
	m.History = res.History
	// The provider's measurement described the pre-compaction request, so it is
	// gone. Re-base on an estimate of the new history: the footer shows the
	// shrink immediately, and the next auto-compact check measures only what is
	// appended from here (see compact.usedTokens).
	m.ContextTokens = int64(compact.Estimate(res.History) + m.CompactConfig(m.Cfg).Overhead)
	m.ContextMsgs = len(m.History)
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
