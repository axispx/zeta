package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/core"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/prompt"
)

// This file owns the agent turn: starting one (submit / beginTurn) and routing
// what it sends back. Live events arrive as turn*Msgs tagged with a turn id;
// dispatchTurnMsg drops the ones that belong to a cancelled or replaced turn.

// dispatchTurnMsg routes live turn events. Returns (cmd, true) when msg was a
// turn*Msg (including stale ones dropped by turn id).
func (m *Model) dispatchTurnMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case turnDeltaMsg:
		if !m.markStreamed(msg.id) {
			return nil, true
		}
		return m.handleTurnDelta(msg), true
	case turnReasoningMsg:
		if !m.markStreamed(msg.id) {
			return nil, true
		}
		return m.handleTurnReasoning(msg), true
	case turnAssistantMsg:
		if !m.markStreamed(msg.id) {
			return nil, true
		}
		return m.handleTurnAssistant(msg), true
	case turnToolStartMsg:
		if !m.markEffects(msg.id) {
			return nil, true
		}
		return m.handleTurnToolStart(msg), true
	case turnToolOutMsg:
		if !m.markEffects(msg.id) {
			return nil, true
		}
		return m.handleTurnToolOut(msg), true
	case turnToolMsg:
		if !m.markEffects(msg.id) {
			return nil, true
		}
		return m.handleTurnTool(msg), true
	case turnDoneMsg:
		if !m.turn.live(msg.id) {
			return nil, true
		}
		return m.handleTurnDone(), true
	case turnErrMsg:
		if !m.turn.live(msg.id) {
			return nil, true
		}
		return m.handleTurnErr(msg.err), true
	default:
		return nil, false
	}
}

// markStreamed is live plus the replay gate: visible output means a later 401
// must not re-run the agent loop.
func (m *Model) markStreamed(id int) bool {
	if !m.turn.live(id) {
		return false
	}
	m.session.MarkStreamed()
	return true
}

// markEffects is live plus the replay gate for tool work.
func (m *Model) markEffects(id int) bool {
	if !m.turn.live(id) {
		return false
	}
	m.session.MarkEffects()
	return true
}

// planFraming is true when this turn is Plan mode. Mode is frozen while a turn
// runs, so the same snapshot is used for the live agent row and JSONL persist.
// Stored on the message so framing survives later mode switches and resume.
func (m *Model) planFraming() bool {
	return m.session.Mode == prompt.ModePlan
}

func (m *Model) handleTurnDelta(msg turnDeltaMsg) tea.Cmd {
	if m.turn.current == nil {
		return nil
	}
	m.turn.current.beginStreaming() // clears thinking; pending/next paint drops the chrome
	n := len(m.transcript.messages)
	if n > 0 && m.transcript.messages[n-1].Role == RoleAgent {
		m.transcript.messages[n-1].Text += msg.text
	} else {
		m.transcript.messages = append(m.transcript.messages, Message{
			Role:      RoleAgent,
			Text:      msg.text,
			framePlan: m.planFraming(),
		})
	}
	// Ingest every token; paint at most every streamPaintEvery.
	return tea.Batch(m.requestStreamPaint(), waitTurn(m.turn.current))
}

// handleTurnReasoning appends pre-answer reasoning for the live tail.
// Outside thinkingPhase tokens are ignored; the stream is still drained.
func (m *Model) handleTurnReasoning(msg turnReasoningMsg) tea.Cmd {
	if m.turn.current == nil {
		return nil
	}
	if !m.turn.current.acceptReasoning(msg.text) {
		return waitTurn(m.turn.current)
	}
	return tea.Batch(m.requestStreamPaint(), waitTurn(m.turn.current))
}

func (m *Model) handleTurnAssistant(msg turnAssistantMsg) tea.Cmd {
	if m.turn.current == nil {
		return nil
	}
	// Segment done — flush buffered answer / clear thinking immediately.
	if m.turn.current.endStreaming() {
		m.refreshTranscript()
	}
	m.reportSaveErr(m.session.CommitAssistant(msg.message, msg.usage))
	m.noteProducedPlan(msg.message.Text)
	return waitTurn(m.turn.current)
}

func (m *Model) handleTurnToolStart(msg turnToolStartMsg) tea.Cmd {
	if m.turn.current == nil {
		return nil
	}
	m.turn.current.endStreaming()
	label := msg.label
	if label == "" {
		label = msg.name
	}
	m.transcript.messages = append(m.transcript.messages, newToolMessage(label, msg.name))
	m.turn.current.activeTool = len(m.transcript.messages) - 1
	if detail := strings.TrimSpace(msg.detail); detail != "" {
		m.transcript.messages[m.turn.current.activeTool].Out = detail
	}
	m.refreshTranscript()

	// Agent only waits when core.Classify matches Gate — do not send a Reply it isn't awaiting.
	wait := core.Classify(m.session.Rules, m.session.Grants, m.session.WS.Abs, msg.name, msg.args)
	// Calls the harness settles without the user (policy deny) are answered here.
	if r, ok := core.AutoReply(wait); ok {
		m.transcript.messages[m.turn.current.activeTool].Status = ToolDenied
		m.sendReply(r)
		m.refreshTranscript()
		return waitTurn(m.turn.current)
	}
	switch wait {
	case core.WaitInteractive:
		m.openInteractiveTool(msg.name, msg.args)
	case core.WaitPermission:
		p := newPermissionPrompt(label, msg.name, msg.path)
		p.setArgs(msg.args, m.session.WS.Abs)
		m.panel.setPerm(p)
		m.afterPanelChange()
	}
	return waitTurn(m.turn.current)
}

func (m *Model) handleTurnToolOut(msg turnToolOutMsg) tea.Cmd {
	if m.turn.current == nil {
		return nil
	}
	if i := m.turn.current.activeTool; i >= 0 && i < len(m.transcript.messages) && m.transcript.messages[i].Tool == msg.name {
		m.transcript.messages[i].Out = msg.text
		return tea.Batch(m.requestStreamPaint(), waitTurn(m.turn.current))
	}
	return waitTurn(m.turn.current)
}

func (m *Model) handleTurnTool(msg turnToolMsg) tea.Cmd {
	if m.turn.current == nil {
		return nil
	}
	if i := m.turn.current.activeTool; i >= 0 && i < len(m.transcript.messages) && m.transcript.messages[i].Tool == msg.name {
		if msg.denied {
			m.transcript.messages[i].Status = ToolDenied
		} else {
			m.transcript.messages[i].Status = ToolOK
			if toolHasOut(m.transcript.messages[i].Tool) {
				m.transcript.messages[i].Out = msg.message.Text
			}
			m.refreshSessionDiff()
		}
	}
	m.turn.current.activeTool = -1
	m.reportSaveErr(m.session.CommitTool(msg.message, msg.label, msg.name, msg.denied))
	m.refreshTranscript()
	return waitTurn(m.turn.current)
}

// submit appends the user turn, starts a streaming completion, and refreshes.
// When the transcript is near the context limit, auto-compacts first.
// text/imgs come from parseComposer (inline [Image N] tokens stripped from text).
func (m *Model) submit(text string, imgs []image.Ref) tea.Cmd {
	// Refuse before committing anything: a turn that cannot be sent must stay
	// out of history and off disk, and its text stays in the composer so the
	// user can retry it after /config instead of retyping.
	if m.session.Client == nil {
		m.noteError("no provider configured, run /config to connect one")
		return nil
	}
	// Exclusive jobs / OAuth recover own the busy slot — callers should queue
	// or no-op first; this is the last line of defense against a competing turn.
	if m.exclusiveJob() || m.session.AuthRetrying {
		return nil
	}
	if err := m.session.EnsureFreshClient(context.Background()); err != nil {
		m.noteError(err.Error())
		return nil
	}
	m.session.AuthRetried = false

	m.commitUserPrompt(text, imgs)
	// Keep an in-progress follow-up edit in the composer (drain of another item).
	if m.queue.editID == 0 {
		m.resetInput()
	}
	m.refreshTranscript()
	// Sending is intentional navigation: always show the new user turn, even if
	// the user had scrolled up to read earlier context (stream paints stay put).
	m.transcript.viewport.GotoBottom()

	titlePrompt := text
	if titlePrompt == "" && len(imgs) > 0 {
		titlePrompt = transcriptLabel(imgs[0], 1)
	}
	// Fresh before auto-compact estimate and the turn that follows.
	m.session.RefreshWorkspace()
	if m.session.ShouldAutoCompact(m.session.Client, m.session.Cfg) {
		return m.runCompact(compactAuto, titlePrompt)
	}
	return m.beginTurn(titlePrompt)
}

// beginTurn starts the agent loop for the current history.
// Callers must RefreshWorkspace first (or have just done so).
func (m *Model) beginTurn(titlePrompt string) tea.Cmd {
	// A new turn: clear the replay gate so a 401 before any output can retry.
	m.session.BeginTurn()
	if m.session.Client == nil {
		return nil
	}
	// Defensive: never orphan an in-flight agent loop (e.g. race with submit).
	if m.turn.current != nil {
		m.finishTurn()
	}
	var cmds []tea.Cmd
	cmds = append(cmds, m.turn.start(m.session.Client, &m.session), m.spinner.Tick)
	// Busy gap grows (GapBeforeInput → busyStatusRows); shrink transcript now.
	m.layoutPreservingBottom()
	if titleCmd := m.ensureTitle(titlePrompt); titleCmd != nil {
		cmds = append(cmds, titleCmd)
	}
	return tea.Batch(cmds...)
}
