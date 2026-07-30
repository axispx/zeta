package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/styles"
)

// turnEnd is how an in-flight agent turn stopped.
type turnEnd int

const (
	// turnEndComplete: keep an unconsumed offer for drain as the next turn.
	turnEndComplete turnEnd = iota
	// turnEndAbort: cancel or error — drop unconsumed offer; do not auto-drain.
	turnEndAbort
)

// endTurn is the single turn-lifecycle entrypoint. Queue drain on success is
// handled by handleTurnDone after this returns; abort abandons offers.
func (m *Model) endTurn(kind turnEnd) {
	if m.turn == nil {
		return
	}
	if kind == turnEndAbort {
		m.abandonSteering()
		m.finishTurn()
		m.afterQueueChange()
		return
	}
	m.finishTurn()
}

// finishTurn tears down agent state only. Prefer endTurn for policy-aware exits.
func (m *Model) finishTurn() {
	if m.turn == nil {
		return
	}
	m.cancelStreamPaint() // invalidate pending ticks (gen is on Model)
	// Deny any open permission/ask so the agent unblocks on the same path as user deny.
	m.abandonBottom()
	// Cancel mid-tool: close out the open row — late KindTool is dropped.
	if i := m.turn.activeTool; i >= 0 && i < len(m.messages) && m.messages[i].Status == ToolRunning {
		m.messages[i].Status = ToolDenied
	}
	m.turn.cancel()
	m.turn = nil
	m.history = compact.TrimIncomplete(m.history)
}

func (m *Model) handleTurnErr(err error) {
	if m.turn == nil {
		return
	}
	m.endTurn(turnEndAbort)
	errMsg := Message{Role: RoleError, Text: err.Error()}
	m.messages = append(m.messages, errMsg)
	m.persist(session.Record{Role: session.RoleError, Text: errMsg.Text})
	m.refreshTranscript()
}

const (
	followUpsTitle   = "follow-ups"
	followUpsBullet  = "○ "
	queueListMaxRows = 6
)

// queuedPrompt is a follow-up not yet committed to transcript/history.
type queuedPrompt struct {
	text    string
	imgs    []image.Ref
	display string
}

func newQueuedPrompt(text string, imgs []image.Ref) queuedPrompt {
	return queuedPrompt{
		text:    text,
		imgs:    imgs,
		display: userDisplayText(text, imgs),
	}
}

func (m *Model) clearQueue() {
	m.abandonSteering()
	m.queue = nil
}

func (m *Model) hasQueueState() bool {
	return m.offered != nil || len(m.queue) > 0
}

// followUpItems is offered (if any) then waiting queue — display order.
func (m Model) followUpItems() []queuedPrompt {
	n := len(m.queue)
	if m.offered != nil {
		n++
	}
	if n == 0 {
		return nil
	}
	out := make([]queuedPrompt, 0, n)
	if m.offered != nil {
		out = append(out, *m.offered)
	}
	return append(out, m.queue...)
}

// commitUserPrompt appends one user turn to transcript, API history, and JSONL.
func (m *Model) commitUserPrompt(text string, imgs []image.Ref) {
	display := userDisplayText(text, imgs)
	m.messages = append(m.messages, Message{Role: RoleUser, Text: display})
	m.history = append(m.history, ai.Message{Role: ai.RoleUser, Text: text, Images: imgs})
	m.persist(session.Record{Role: session.RoleUser, Text: text, Images: imgs})
}

func (m *Model) enqueuePrompt(text string, imgs []image.Ref) tea.Cmd {
	m.queue = append(m.queue, newQueuedPrompt(text, imgs))
	m.resetInput()
	m.afterQueueChange()
	return nil
}

func (m *Model) afterQueueChange() {
	if m.ready {
		m.layoutPreservingBottom()
	}
}

// promoteOldestToSteer moves queue[0] onto the steer channel as offered.
// Non-blocking: if the channel is full, queue is left unchanged.
func (m *Model) promoteOldestToSteer() {
	if m.turn == nil || m.offered != nil || len(m.queue) == 0 || m.turn.steers == nil {
		return
	}
	p := m.queue[0]
	msg := ai.Message{Role: ai.RoleUser, Text: p.text, Images: p.imgs}
	select {
	case m.turn.steers <- msg:
		m.queue = m.queue[1:]
		cp := p
		m.offered = &cp
		m.afterQueueChange()
	default:
		// Channel full (invariant break or race); leave queue intact.
	}
}

// acknowledgeSteering commits a steer the agent already accepted.
// msg is the source of truth; offered may already be nil if Esc raced the event.
func (m *Model) acknowledgeSteering(msg ai.Message) {
	m.offered = nil
	m.commitUserPrompt(msg.Text, msg.Images)
	m.refreshTranscript()
}

// drainSteerChannel drops a pending steer message if the agent has not consumed it yet.
func (m *Model) drainSteerChannel() {
	if m.turn == nil || m.turn.steers == nil {
		return
	}
	select {
	case <-m.turn.steers:
	default:
	}
}

// abandonSteering drops an in-flight offer without committing it.
// Used on abort only — successful complete drains an unconsumed offer as a new turn.
func (m *Model) abandonSteering() {
	if m.offered == nil {
		return
	}
	m.drainSteerChannel()
	m.offered = nil
}

// takeNextFollowUp pops the oldest retained prompt (unconsumed offer first).
func (m *Model) takeNextFollowUp() (queuedPrompt, bool) {
	if m.offered != nil {
		p := *m.offered
		m.offered = nil
		return p, true
	}
	if len(m.queue) == 0 {
		return queuedPrompt{}, false
	}
	p := m.queue[0]
	m.queue = m.queue[1:]
	return p, true
}

// drainNextQueuedPrompt starts the oldest retained prompt as a normal turn.
// If submit refuses before committing (no client / oauth), the prompt is restored.
func (m *Model) drainNextQueuedPrompt() tea.Cmd {
	p, ok := m.takeNextFollowUp()
	if !ok {
		return nil
	}
	nHist := len(m.history)
	cmd := m.submit(p.text, p.imgs)
	if len(m.history) == nHist {
		m.queue = append([]queuedPrompt{p}, m.queue...)
		m.afterQueueChange()
	}
	return cmd
}

// discardOldestQueued drops the oldest follow-up (offered first, then queue head).
// Returns false when there is nothing to discard (caller may cancel the turn).
func (m *Model) discardOldestQueued() bool {
	if m.offered != nil {
		m.abandonSteering()
		m.afterQueueChange()
		return true
	}
	if len(m.queue) == 0 {
		return false
	}
	m.queue = m.queue[1:]
	m.afterQueueChange()
	return true
}

func (m *Model) handleTurnDone() tea.Cmd {
	// Late/spurious Done after cancel/error: do not drain remaining queue.
	if m.turn == nil {
		return nil
	}
	m.endTurn(turnEndComplete)
	if cmd := m.drainNextQueuedPrompt(); cmd != nil {
		return cmd
	}
	m.maybeOfferPlan()
	m.refreshTranscript()
	return nil
}

func (m *Model) handleTurnSteerAccepted(msg ai.Message) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	m.acknowledgeSteering(msg)
	return waitTurn(m.turn)
}

func queueItemLabel(p queuedPrompt) string {
	label := strings.ReplaceAll(p.display, "\n", " ")
	if label == "" {
		return "image"
	}
	return label
}

// renderQueueFollowups lists every pending follow-up in the gap slot.
func (m Model) renderQueueFollowups(width int) string {
	items := m.followUpItems()
	if len(items) == 0 {
		return ""
	}
	innerW, _ := overlayWidths(width)
	if innerW < 20 {
		innerW = 20
	}
	fillW := followUpsFillW(innerW)
	row := styles.OverlayRow
	hint := styles.FollowUpsHint
	title := styles.FollowUpsHeader
	border := lipgloss.NewStyle().Foreground(styles.Yellow)

	bulletW := lipgloss.Width(followUpsBullet)
	var lines []string
	show := len(items)
	if show > queueListMaxRows {
		show = queueListMaxRows
	}
	for i := 0; i < show; i++ {
		label := truncateRight(queueItemLabel(items[i]), fillW-bulletW)
		lines = append(lines, row.Width(fillW).Render(followUpsBullet+label))
	}
	if extra := len(items) - show; extra > 0 {
		lines = append(lines, hint.Width(fillW).Render(fmt.Sprintf("… %d more", extra)))
	}
	lines = append(lines, row.Width(fillW).Render(""))
	lines = append(lines, renderFollowUpsFooter(hint))

	boxStyle := styles.FollowUpsBoxBare(innerW)
	body := boxStyle.Render(strings.Join(lines, "\n"))
	top := renderFollowUpsTopLine(innerW, title, border)
	box := lipgloss.NewStyle().
		Margin(0, styles.InputMarginH, styles.InputMarginB, styles.InputMarginH).
		Render(lipgloss.JoinVertical(lipgloss.Left, top, body))
	return lipgloss.JoinVertical(lipgloss.Left, "", box)
}

func renderFollowUpsFooter(hint lipgloss.Style) string {
	sep := hint.Render(" • ")
	parts := []string{
		hint.Render("enter send now"),
		hint.Render("esc cancel"),
	}
	return strings.Join(parts, sep)
}

func renderFollowUpsTopLine(innerW int, title, border lipgloss.Style) string {
	b := lipgloss.NormalBorder()
	titleR := title.Render(followUpsTitle)
	tl := border.Render(b.TopLeft)
	tr := border.Render(b.TopRight)
	dashN := innerW - lipgloss.Width(titleR) - lipgloss.Width(tl) - lipgloss.Width(tr)
	if dashN < 0 {
		dashN = 0
	}
	dashes := border.Render(strings.Repeat(b.Top, dashN))
	line := titleR + tl + dashes + tr
	if pad := innerW - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}

// followUpsFillW is the content width inside the yellow border and padding.
func followUpsFillW(innerW int) int {
	// L/R border (2) + left pad (1) + OverlayPadRight
	w := innerW - 2 - 1 - styles.OverlayPadRight
	if w < 1 {
		return 1
	}
	return w
}
