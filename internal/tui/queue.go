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

// Follow-up queue
//
//	queue[]              waiting items (FIFO; oldest at [0])
//	editID               id open in the composer, or 0
//	queueFocus+queueSel  modal nav over the panel
//
// Keys (outside queue focus):
//
//	Enter   text → send / busy enqueue; empty → deliver head (interrupt if busy)
//	Esc     cancel edit → unfocus → cancel turn (queue kept)
//	Ctrl+C  leave edit/focus → interrupt ladder → clear queue → quit
//	Ctrl+Q  focus queue
//
// Keys (queue focused):
//
//	↑/↓  move · Enter send · e edit · d remove · Esc/Ctrl+Q back
//
// Auto-drain on turn complete when canDrain() (not editing head, composer empty).

// finishTurn tears down in-flight agent state. Does not touch the follow-up queue.
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

const (
	followUpsTitle   = "follow-ups"
	followUpsBullet  = "○ "
	queueListMaxRows = 6
)

// queuedPrompt is a follow-up not yet committed to transcript/history.
type queuedPrompt struct {
	id      int
	text    string
	imgs    []image.Ref
	display string
}

func newQueuedPrompt(id int, text string, imgs []image.Ref) queuedPrompt {
	return queuedPrompt{
		id:      id,
		text:    text,
		imgs:    imgs,
		display: userDisplayText(text, imgs),
	}
}

func (m *Model) allocQueueID() int {
	m.nextQueueID++
	return m.nextQueueID
}

func (m *Model) clearQueue() {
	if m.editID != 0 {
		m.editID = 0
		m.resetInput()
	}
	m.unfocusQueue()
	m.queue = nil
}

func (m *Model) hasQueueState() bool {
	return len(m.queue) > 0 || m.editID != 0
}

// toggleQueueFocus enters/leaves follow-up navigation.
// Returns false when there is nothing to focus.
func (m *Model) toggleQueueFocus() bool {
	if m.queueFocus {
		m.unfocusQueue()
		return true
	}
	return m.focusQueue()
}

func (m *Model) focusQueue() bool {
	if len(m.queue) == 0 || m.editID != 0 {
		return false
	}
	m.queueFocus = true
	// Land on the most recently queued item (last = newest).
	m.queueSel.selected = len(m.queue) - 1
	m.afterQueueChange()
	return true
}

func (m *Model) unfocusQueue() {
	if !m.queueFocus {
		return
	}
	m.queueFocus = false
	m.queueSel.clear()
	m.afterQueueChange()
}

func (m *Model) clampQueueSel() {
	m.queueSel.clamp(len(m.queue))
	if len(m.queue) == 0 {
		m.unfocusQueue()
	}
}

// selectedQueueID is the focused row's id, or 0.
func (m Model) selectedQueueID() int {
	if !m.queueFocus || len(m.queue) == 0 {
		return 0
	}
	i := m.queueSel.selected
	if i < 0 || i >= len(m.queue) {
		return 0
	}
	return m.queue[i].id
}

// handleQueueNavKey handles keys while the follow-ups panel is focused.
// Returns (cmd, true) when the key was consumed.
func (m *Model) handleQueueNavKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.queueFocus {
		return nil, false
	}
	if len(m.queue) == 0 {
		m.unfocusQueue()
		return nil, true
	}
	m.clampQueueSel()
	key := msg.String()
	n := len(m.queue)

	if m.queueSel.move(n, key) {
		m.afterQueueChange()
		return nil, true
	}
	switch key {
	case "enter":
		return m.deliverQueued(m.selectedQueueID()), true
	case "e":
		id := m.selectedQueueID()
		m.unfocusQueue()
		if id != 0 {
			m.beginEdit(id)
		}
		return nil, true
	case "d", "x", "delete", "backspace":
		id := m.selectedQueueID()
		if id != 0 {
			m.removeQueued(id)
			m.clampQueueSel()
		}
		return nil, true
	case "ctrl+q":
		m.unfocusQueue()
		return nil, true
	case "esc":
		// Handled by Esc ladder; keep false so handleQueueEsc runs.
		return nil, false
	default:
		// Swallow other keys while focused so they don't type into the composer.
		return nil, true
	}
}

// deliverQueued submits a follow-up by id. Interrupts a live turn first
// (no "Cancelled" chrome — the follow-up is the next turn). Restores the item
// at its original index if submit refuses before committing.
func (m *Model) deliverQueued(id int) tea.Cmd {
	m.unfocusQueue()
	if id == 0 || m.editID == id {
		return nil
	}
	// OAuth recover owns the busy slot — do not start a competing turn.
	if m.authRetrying {
		return nil
	}
	i := m.queueIndex(id)
	if i < 0 {
		return nil
	}
	p := m.queue[i]
	m.queue = append(m.queue[:i], m.queue[i+1:]...)
	m.afterQueueChange()

	if m.turn != nil {
		m.finishTurn()
	}

	nHist := len(m.history)
	cmd := m.submit(p.text, p.imgs)
	if len(m.history) == nHist {
		if i > len(m.queue) {
			i = len(m.queue)
		}
		m.queue = append(m.queue[:i:i], append([]queuedPrompt{p}, m.queue[i:]...)...)
		m.afterQueueChange()
	}
	return cmd
}

// queueHeadID is the id of the next waiting item, or 0.
func (m Model) queueHeadID() int {
	if len(m.queue) == 0 {
		return 0
	}
	return m.queue[0].id
}

// editingHead is true when the composer holds the next item that would drain.
func (m Model) editingHead() bool {
	return m.editID != 0 && m.editID == m.queueHeadID()
}

// canDrain reports whether auto-start / empty-Enter may take the next follow-up.
// Editing a non-head item does not block; a non-empty composer draft does.
func (m Model) canDrain() bool {
	if len(m.queue) == 0 || m.editingHead() {
		return false
	}
	if m.editID == 0 && !m.composerIsEmpty() {
		return false
	}
	return true
}

func (m Model) composerIsEmpty() bool {
	text, imgs := m.parseComposer()
	return text == "" && len(imgs) == 0
}

func (m *Model) queueIndex(id int) int {
	for i := range m.queue {
		if m.queue[i].id == id {
			return i
		}
	}
	return -1
}

// commitUserPrompt appends one user turn to transcript, API history, and JSONL.
func (m *Model) commitUserPrompt(text string, imgs []image.Ref) {
	display := userDisplayText(text, imgs)
	m.messages = append(m.messages, Message{Role: RoleUser, Text: display})
	m.history = append(m.history, ai.Message{Role: ai.RoleUser, Text: text, Images: imgs})
	m.persist(session.Record{Role: session.RoleUser, Text: text, Images: imgs})
}

// enqueuePrompt appends a waiting follow-up. No-op while editing.
func (m *Model) enqueuePrompt(text string, imgs []image.Ref) tea.Cmd {
	if m.editID != 0 {
		return nil
	}
	m.queue = append(m.queue, newQueuedPrompt(m.allocQueueID(), text, imgs))
	m.resetInput()
	m.afterQueueChange()
	return nil
}

func (m *Model) afterQueueChange() {
	if m.ready {
		m.layoutPreservingBottom()
	}
}

// drainNextQueuedPrompt starts the oldest waiting prompt as a normal turn.
// Busy turns are interrupted first. No-op when canDrain is false.
func (m *Model) drainNextQueuedPrompt() tea.Cmd {
	if !m.canDrain() {
		return nil
	}
	return m.deliverQueued(m.queueHeadID())
}

// beginEdit opens a waiting item in the composer. Item stays in queue[].
func (m *Model) beginEdit(id int) bool {
	if id == 0 || m.editID != 0 {
		return false
	}
	i := m.queueIndex(id)
	if i < 0 {
		return false
	}
	p := m.queue[i]
	m.unfocusQueue()
	m.editID = id
	m.loadQueuedIntoComposer(p)
	m.afterQueueChange()
	return true
}

// saveEdit writes the composer back onto the item being edited.
func (m *Model) saveEdit(text string, imgs []image.Ref) bool {
	if m.editID == 0 {
		return false
	}
	i := m.queueIndex(m.editID)
	if i < 0 {
		m.editID = 0
		m.resetInput()
		return false
	}
	m.queue[i] = newQueuedPrompt(m.editID, text, imgs)
	m.editID = 0
	m.resetInput()
	m.afterQueueChange()
	return true
}

// cancelEdit clears the composer and leaves the queue item unchanged.
func (m *Model) cancelEdit() bool {
	if m.editID == 0 {
		return false
	}
	m.editID = 0
	m.resetInput()
	m.afterQueueChange()
	return true
}

// removeQueued drops a waiting item by id. If it was being edited, clears edit.
func (m *Model) removeQueued(id int) bool {
	if id == 0 {
		return false
	}
	i := m.queueIndex(id)
	if i < 0 {
		return false
	}
	m.queue = append(m.queue[:i], m.queue[i+1:]...)
	if m.editID == id {
		m.editID = 0
		m.resetInput()
	}
	if len(m.queue) == 0 {
		m.unfocusQueue()
	} else if m.queueFocus {
		m.clampQueueSel()
	}
	m.afterQueueChange()
	return true
}

// loadQueuedIntoComposer puts a follow-up's text and images into the input.
func (m *Model) loadQueuedIntoComposer(p queuedPrompt) {
	m.resetPromptHistory()
	m.clearPendingImages()
	if len(p.imgs) == 0 {
		m.setPromptValue(p.text)
		return
	}
	// Rebuild [Image N] tokens so parseComposer round-trips attachments.
	m.pendingImages = make(map[int]image.Ref, len(p.imgs))
	var b strings.Builder
	b.WriteString(p.text)
	for _, img := range p.imgs {
		m.nextImageN++
		n := m.nextImageN
		m.pendingImages[n] = img
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(imageToken(n))
	}
	m.setPromptValue(b.String())
}

// handleQueueEsc runs the follow-up branch of the Esc/Ctrl+C ladder.
// Returns true when consumed (caller should not cancel the turn / quit).
func (m *Model) handleQueueEsc() bool {
	if m.cancelEdit() {
		return true
	}
	if m.queueFocus {
		m.unfocusQueue()
		return true
	}
	return false
}

func (m *Model) handleTurnDone() tea.Cmd {
	// Late/spurious Done after cancel/error: do not drain remaining queue.
	if m.turn == nil {
		return nil
	}
	m.finishTurn()
	if cmd := m.drainNextQueuedPrompt(); cmd != nil {
		return cmd
	}
	m.maybeOfferPlan()
	m.refreshTranscript()
	return nil
}

func queueItemLabel(p queuedPrompt) string {
	label := strings.ReplaceAll(p.display, "\n", " ")
	if label == "" {
		return "image"
	}
	return label
}

// renderQueueFollowups lists waiting follow-ups in the gap slot.
func (m Model) renderQueueFollowups(width int) string {
	waiting := m.queue
	if len(waiting) == 0 {
		return ""
	}
	innerW, _ := overlayWidths(width)
	if innerW < 20 {
		innerW = 20
	}
	fillW := followUpsFillW(innerW)
	row := styles.OverlayRow
	selRow := styles.AccentRowSelected
	hint := styles.FollowUpsHint
	border := lipgloss.NewStyle().Foreground(styles.Yellow)

	var lines []string
	rowsLeft := queueListMaxRows

	sel := m.queueSel.selected
	if !m.queueFocus || sel < 0 {
		sel = 0
	}
	if n := len(waiting); n > 0 && sel >= n {
		sel = n - 1
	}
	start := 0
	show := len(waiting)
	if show > rowsLeft {
		show = rowsLeft
		start = sel - show/2
		if start < 0 {
			start = 0
		}
		if start+show > len(waiting) {
			start = len(waiting) - show
		}
	}
	for i := 0; i < show; i++ {
		idx := start + i
		p := waiting[idx]
		label := queueItemLabel(p)
		focused := m.queueFocus && idx == sel
		editing := m.editID != 0 && p.id == m.editID
		prefix := followUpsBullet
		switch {
		case editing:
			prefix = "✎ "
		case focused:
			prefix = "→ "
		}
		pw := lipgloss.Width(prefix)
		label = truncateRight(label, fillW-pw)
		line := prefix + label
		if focused {
			lines = append(lines, selRow.Width(fillW).Render(line))
		} else {
			lines = append(lines, row.Width(fillW).Render(line))
		}
	}
	if start > 0 || start+show < len(waiting) {
		hidden := len(waiting) - show
		lines = append(lines, hint.Width(fillW).Render(fmt.Sprintf("… %d more", hidden)))
	}
	if len(lines) == 0 {
		return ""
	}
	lines = append(lines, row.Width(fillW).Render(""))
	lines = append(lines, renderFollowUpsFooter(hint, m.editID != 0, m.queueFocus))

	boxStyle := styles.FollowUpsBoxBare(innerW)
	body := boxStyle.Render(strings.Join(lines, "\n"))
	topTitle := followUpsTitle
	switch {
	case m.editID != 0:
		topTitle = "follow-ups · editing"
	case m.queueFocus:
		topTitle = "follow-ups · ↑/↓"
	}
	top := renderFollowUpsTopLine(innerW, styles.FollowUpsHeader.Render(topTitle), border)
	box := lipgloss.NewStyle().
		Margin(0, styles.InputMarginH, styles.InputMarginB, styles.InputMarginH).
		Render(lipgloss.JoinVertical(lipgloss.Left, top, body))
	return lipgloss.JoinVertical(lipgloss.Left, "", box)
}

func renderFollowUpsFooter(hint lipgloss.Style, editing, focused bool) string {
	sep := hint.Render(" • ")
	var parts []string
	switch {
	case editing:
		parts = []string{
			hint.Render("enter save"),
			hint.Render("esc cancel edit"),
		}
	case focused:
		parts = []string{
			hint.Render("enter send"),
			hint.Render("e edit"),
			hint.Render("d remove"),
			hint.Render("esc back"),
		}
	default:
		parts = []string{
			hint.Render("enter send now"),
			hint.Render("ctrl+q manage"),
			hint.Render("esc cancel turn"),
		}
	}
	return strings.Join(parts, sep)
}

func renderFollowUpsTopLine(innerW int, titleR string, border lipgloss.Style) string {
	b := lipgloss.NormalBorder()
	tr := border.Render(b.TopRight)
	dashN := innerW - lipgloss.Width(titleR) - lipgloss.Width(tr)
	if dashN < 0 {
		dashN = 0
	}
	dashes := border.Render(strings.Repeat(b.Top, dashN))
	line := titleR + dashes + tr
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
