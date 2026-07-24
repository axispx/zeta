package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/prompt"
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

// busy reports whether a turn or compaction is in flight.
func (m *Model) busy() bool {
	return m.turn != nil || m.compacting
}

// compactConfig builds thresholds from the active model and prompt overhead.
func (m *Model) compactConfig() compact.Config {
	overhead := compact.Estimate([]ai.Message{
		{Role: ai.RoleSystem, Text: prompt.System(m.ws)},
		{Role: ai.RoleDeveloper, Text: m.mode.Instructions()},
	})
	return compact.Config{
		ContextWindow: m.cfg.ContextWindow(),
		Overhead:      overhead + compact.DefaultToolsOverhead,
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
		m.noteError("oauth refresh: " + err.Error())
		return nil
	}
	return m.runCompact(compactManual, "")
}

// runCompact starts an async compact. Auto kind continues into a turn when done.
func (m *Model) runCompact(kind compactKind, titlePrompt string) tea.Cmd {
	hist := append([]ai.Message(nil), m.history...)
	cfg := m.compactConfig()
	client := m.client
	force := kind == compactManual

	ctx, cancel := context.WithCancel(context.Background())
	m.compactCancel = cancel
	m.compacting = true

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
		return m.beginTurn(msg.titlePrompt)
	}
	return nil
}

func (m *Model) applyCompactResult(res compact.Result) {
	m.history = res.History
	// Estimate remaining context so the footer reflects the shrink until next API usage.
	m.contextTokens = int64(compact.Estimate(res.History) + m.compactConfig().Overhead)
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
