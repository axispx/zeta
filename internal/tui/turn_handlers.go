package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/classifier"
	"github.com/axispx/zeta/internal/harness"
	"github.com/axispx/zeta/internal/image"
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
	case turnSteerMsg:
		if !m.markEffects(msg.id) {
			return nil, true
		}
		return m.handleTurnSteer(msg), true
	case reviewDoneMsg:
		if !m.turn.live(msg.id) {
			return nil, true
		}
		return m.handleReviewDone(msg), true
	case turnCompactMsg:
		if !m.turn.live(msg.id) {
			return nil, true
		}
		return m.handleTurnCompact(), true
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
			Role: RoleAgent,
			Text: msg.text,
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
	m.reportSaveErr(m.session.CommitAssistant(msg.message, msg.usage, msg.plan))
	return waitTurn(m.turn.current)
}

// handleTurnSteer records a message the loop took in mid-turn: it leaves the
// pending list and becomes a user turn in the transcript and history.
func (m *Model) handleTurnSteer(msg turnSteerMsg) tea.Cmd {
	if m.turn.current == nil {
		return nil
	}
	m.commitUserPrompt(msg.message.Text, msg.message.Images)
	m.afterQueueChange()
	m.refreshTranscript()
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

	// Agent only waits when harness.Classify matches Gate — do not send a Reply it isn't awaiting.
	wait := harness.Classify(m.session.Rules, m.session.Grants, m.session.WS.Abs, msg.name, msg.args)
	// Calls the harness settles without the user (policy deny) are answered here.
	if r, ok := harness.AutoReply(wait); ok {
		m.transcript.messages[m.turn.current.activeTool].Status = ToolDenied
		m.sendReply(r)
		m.refreshTranscript()
		return waitTurn(m.turn.current)
	}
	switch wait {
	case harness.WaitInteractive:
		m.openInteractiveTool(msg.name, msg.args)
	case harness.WaitPermission:
		// A command the reviewer may settle waits for its verdict before the
		// prompt opens, so an approved one never flashes a prompt.
		if review := m.startReview(msg); review != nil {
			return tea.Batch(review, waitTurn(m.turn.current))
		}
		m.openPermission(label, msg, "")
	}
	return waitTurn(m.turn.current)
}

// openPermission shows the approval prompt for a gated tool start. review is
// the auto review's account of why it is asking, or "" when none ran.
func (m *Model) openPermission(label string, msg turnToolStartMsg, review string) {
	p := newPermissionPrompt(label, msg.name, msg.path)
	p.review = review
	p.setArgs(m.session.Rules.Policy(), msg.args, m.session.WS.Abs)
	m.panel.setPerm(p)
	m.afterPanelChange()
}

// reviewDoneMsg carries the reviewer's verdict for the tool start that asked.
// id is the turn it belongs to, so a verdict for a cancelled turn is dropped.
type reviewDoneMsg struct {
	id      int
	start   turnToolStartMsg
	verdict classifier.Verdict
}

// startReview begins the auto review of a gated shell command, or returns nil
// when review is off or this call is not one the reviewer may see.
func (m *Model) startReview(msg turnToolStartMsg) tea.Cmd {
	rv := m.session.Reviewer()
	if rv == nil {
		return nil
	}
	req, ok := m.session.ReviewRequest(msg.name, msg.args)
	if !ok {
		return nil
	}
	id := m.turn.current.id
	return func() tea.Msg {
		return reviewDoneMsg{id: id, start: msg, verdict: rv.Review(context.Background(), req)}
	}
}

// handleReviewDone runs an approved command, and otherwise opens the prompt the
// call would have had without a review. The loop is blocked on the reply either
// way, so exactly one of the two answers it.
func (m *Model) handleReviewDone(msg reviewDoneMsg) tea.Cmd {
	// An approval is silent. Anything else shows why it is asking inside the
	// prompt, so the prompt is never mistaken for "review is off".
	if msg.verdict.Approved {
		m.sendReply(harness.RunTool())
		return nil
	}
	label := msg.start.label
	if label == "" {
		label = msg.start.name
	}
	m.openPermission(label, msg.start, msg.verdict.Summary())
	return nil
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
	return m.submitAfter(nil, text, imgs)
}

// submitAfter is submit preceded by earlier user prompts, each committed as its
// own turn in history, so several queued follow-ups reach the model in one
// request without being merged. Nothing is committed if the send is refused.
func (m *Model) submitAfter(lead []queuedPrompt, text string, imgs []image.Ref) tea.Cmd {
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

	for _, p := range lead {
		m.commitUserPrompt(p.text, p.imgs)
	}
	m.commitUserPrompt(text, imgs)
	m.resetInput()
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
