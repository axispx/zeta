package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/prompt"
)

// dispatchTurnMsg routes live turn events. Returns (cmd, true) when msg was a
// turn*Msg (including stale ones dropped by turn id).
func (m *Model) dispatchTurnMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case turnDeltaMsg:
		if !m.turnMsgLive(msg.id) {
			return nil, true
		}
		return m.handleTurnDelta(msg), true
	case turnReasoningMsg:
		if !m.turnMsgLive(msg.id) {
			return nil, true
		}
		return m.handleTurnReasoning(msg), true
	case turnAssistantMsg:
		if !m.turnMsgLive(msg.id) {
			return nil, true
		}
		return m.handleTurnAssistant(msg), true
	case turnToolStartMsg:
		if !m.turnMsgLive(msg.id) {
			return nil, true
		}
		return m.handleTurnToolStart(msg), true
	case turnToolOutMsg:
		if !m.turnMsgLive(msg.id) {
			return nil, true
		}
		return m.handleTurnToolOut(msg), true
	case turnToolMsg:
		if !m.turnMsgLive(msg.id) {
			return nil, true
		}
		return m.handleTurnTool(msg), true
	case turnDoneMsg:
		if !m.turnMsgLive(msg.id) {
			return nil, true
		}
		return m.handleTurnDone(), true
	case turnErrMsg:
		if !m.turnMsgLive(msg.id) {
			return nil, true
		}
		m.handleTurnErr(msg.err)
		return nil, true
	default:
		return nil, false
	}
}

// turnMsgLive reports whether a turn*Msg still belongs to the active turn.
// Late events from a cancelled/replaced turn must not mutate state.
func (m Model) turnMsgLive(id int) bool {
	return m.turn != nil && m.turn.id == id
}

// planFraming is true when this turn is Plan mode. Mode is frozen while a turn
// runs, so the same snapshot is used for the live agent row and JSONL persist.
// Stored on the message so framing survives later mode switches and resume.
func (m *Model) planFraming() bool {
	return m.mode == prompt.ModePlan
}

func (m *Model) handleTurnDelta(msg turnDeltaMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	m.turn.beginStreaming() // clears thinking; pending/next paint drops the chrome
	n := len(m.messages)
	if n > 0 && m.messages[n-1].Role == RoleAgent {
		m.messages[n-1].Text += msg.text
	} else {
		m.messages = append(m.messages, Message{
			Role:      RoleAgent,
			Text:      msg.text,
			framePlan: m.planFraming(),
		})
	}
	// Ingest every token; paint at most every streamPaintEvery.
	return tea.Batch(m.requestStreamPaint(), waitTurn(m.turn))
}

// handleTurnReasoning appends pre-answer reasoning for the live tail.
// Outside thinkingPhase tokens are ignored; the stream is still drained.
func (m *Model) handleTurnReasoning(msg turnReasoningMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	if !m.turn.acceptReasoning(msg.text) {
		return waitTurn(m.turn)
	}
	return tea.Batch(m.requestStreamPaint(), waitTurn(m.turn))
}

func (m *Model) handleTurnAssistant(msg turnAssistantMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	// Segment done — flush buffered answer / clear thinking immediately.
	if m.turn.endStreaming() {
		m.refreshTranscript()
	}
	m.history = append(m.history, msg.message)
	if n := msg.usage.ContextTokens(); n > 0 {
		m.contextTokens = n
	}
	// Full assistant text (including <proposed_plan>) on the agent row for UI,
	// JSONL, and API history. FramePlan snapshots planFraming at ingest.
	rec := recordFromAPI(msg.message)
	rec.FramePlan = m.planFraming()
	m.persist(rec)
	m.noteProducedPlan(msg.message.Text)
	return waitTurn(m.turn)
}

func (m *Model) handleTurnToolStart(msg turnToolStartMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	m.turn.endStreaming()
	label := msg.label
	if label == "" {
		label = msg.name
	}
	m.messages = append(m.messages, newToolMessage(label, msg.name))
	m.turn.activeTool = len(m.messages) - 1
	if detail := strings.TrimSpace(msg.detail); detail != "" {
		m.messages[m.turn.activeTool].Out = detail
	}
	m.refreshTranscript()

	// Agent only waits when waitFor matches Gate — do not send a Reply it isn't awaiting.
	switch waitFor(msg.name, m.grants) {
	case waitInteractive:
		m.openInteractiveTool(msg.name, msg.args)
	case waitPermission:
		m.bottom.setPerm(newPermissionPrompt(label, msg.name, msg.path))
		m.afterSetBottom()
	}
	return waitTurn(m.turn)
}

func (m *Model) handleTurnToolOut(msg turnToolOutMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	if i := m.turn.activeTool; i >= 0 && i < len(m.messages) && m.messages[i].Tool == msg.name {
		m.messages[i].Out = msg.text
		return tea.Batch(m.requestStreamPaint(), waitTurn(m.turn))
	}
	return waitTurn(m.turn)
}

func (m *Model) handleTurnTool(msg turnToolMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	m.history = append(m.history, msg.message)
	if i := m.turn.activeTool; i >= 0 && i < len(m.messages) && m.messages[i].Tool == msg.name {
		if msg.denied {
			m.messages[i].Status = ToolDenied
		} else {
			m.messages[i].Status = ToolOK
			if toolHasOut(m.messages[i].Tool) {
				m.messages[i].Out = msg.message.Text
			}
			m.refreshSessionDiff()
		}
	}
	m.turn.activeTool = -1
	m.persist(toolRecord(msg.message, msg.label, msg.name, msg.denied))
	m.refreshTranscript()
	return waitTurn(m.turn)
}
