package tui

import (
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/styles"
)

// Follow-ups while the agent is busy come in two kinds:
//
//	steer (Enter)  joins the running turn at its next tool boundary; held in
//	               the turn's steerBox until the loop takes it
//	queue (Tab)    starts its own turn once this one ends, oldest first
//
// Keys:
//
//	Esc          with steers pending: interrupt and send the oldest now (the
//	             rest stay pending); otherwise
//	             cancel, and anything waiting returns to the composer
//	Alt+↑/↓      copy queued messages into the composer, newest first; ↓ goes newer
//	Ctrl+C       interrupt ladder → clear queue → quit
//
// A steer the loop never took (the turn ended first) is sent as the next turn.

// queue holds queued follow-ups, oldest first.
type queue struct {
	prompts []queuedPrompt
	// recalled is the follow-up last copied into the composer and recalledAt
	// its queue index; resetInput forgets it.
	recalled   *queuedPrompt
	recalledAt int
}

// steerBox is the hand-off between the composer and the running loop. The UI
// pushes; the loop goroutine takes. Both ends see the same pending list.
type steerBox struct {
	mu      sync.Mutex
	pending []queuedPrompt
}

func (b *steerBox) push(p queuedPrompt) {
	b.mu.Lock()
	b.pending = append(b.pending, p)
	b.mu.Unlock()
}

// drain removes and returns everything pending.
func (b *steerBox) drain() []queuedPrompt {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.pending
	b.pending = nil
	return p
}

// take is the loop's view of drain: pending steers as user messages.
func (b *steerBox) take() []ai.Message {
	var out []ai.Message
	for _, p := range b.drain() {
		out = append(out, ai.Message{Role: ai.RoleUser, Text: p.text, Images: p.imgs})
	}
	return out
}

func (b *steerBox) snapshot() []queuedPrompt {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]queuedPrompt(nil), b.pending...)
}

func (b *steerBox) len() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

// mergePrompts joins prompts into one: text one per line, images together.
func mergePrompts(ps []queuedPrompt) queuedPrompt {
	var texts []string
	var imgs []image.Ref
	for _, p := range ps {
		if p.text != "" {
			texts = append(texts, p.text)
		}
		imgs = append(imgs, p.imgs...)
	}
	return newQueuedPrompt(strings.Join(texts, "\n"), imgs)
}

// finishTurn tears down in-flight agent state. Does not touch the follow-up queue.
func (m *Model) finishTurn() {
	if m.turn.current == nil {
		return
	}
	m.cancelStreamPaint() // invalidate pending ticks (gen is on Model)
	// Deny any open permission/ask so the agent unblocks on the same path as user deny.
	m.abandonPanel()
	// Cancel mid-tool: close out the open row — late KindTool is dropped.
	if i := m.turn.current.activeTool; i >= 0 && i < len(m.transcript.messages) && m.transcript.messages[i].Status == ToolRunning {
		m.transcript.messages[i].Status = ToolDenied
	}
	m.turn.cancel()
	m.session.History = compact.TrimIncomplete(m.session.History)
}

const queueLineLimit = 3

// queuedPrompt is a follow-up not yet committed to transcript/history.
type queuedPrompt struct {
	text    string
	imgs    []image.Ref
	display string
}

func newQueuedPrompt(text string, imgs []image.Ref) queuedPrompt {
	return queuedPrompt{text: text, imgs: imgs, display: userDisplayText(text, imgs)}
}

func (m *Model) clearQueue() {
	m.queue.prompts = nil
	m.queue.recalled = nil
}

// hasState reports whether anything is queued.
func (q *queue) hasState() bool {
	return len(q.prompts) > 0
}

// recallQueued is Alt+↑: it copies one queued follow-up into the composer,
// newest first and one older per press. The queue is untouched, so sending the
// copy queues it again at the bottom while the original stays. A composer
// holding anything but the last recalled copy is left alone.
func (m *Model) recallQueued() bool {
	q := &m.queue
	n := len(q.prompts)
	if n == 0 {
		return false
	}
	text, imgs := m.parseComposer()
	at := n - 1
	switch {
	case q.recalled != nil:
		if text != q.recalled.text || len(imgs) != len(q.recalled.imgs) {
			return false
		}
		at = max(min(q.recalledAt, n)-1, 0)
	case text != "" || len(imgs) > 0:
		return false
	}
	p := q.prompts[at]
	m.loadQueuedIntoComposer(p)
	q.recalled, q.recalledAt = &p, at
	m.composer.textarea.MoveToEnd()
	return true
}

// recallNewerQueued is Alt+↓: one step back toward the newest queued copy, and
// past the newest it empties the composer. Needs an unedited recalled copy.
func (m *Model) recallNewerQueued() bool {
	q := &m.queue
	if q.recalled == nil {
		return false
	}
	text, imgs := m.parseComposer()
	if text != q.recalled.text || len(imgs) != len(q.recalled.imgs) {
		return false
	}
	if q.recalledAt >= len(q.prompts)-1 {
		m.resetInput()
		return true
	}
	p := q.prompts[q.recalledAt+1]
	m.loadQueuedIntoComposer(p)
	q.recalled, q.recalledAt = &p, q.recalledAt+1
	m.composer.textarea.MoveToEnd()
	return true
}

// withDraft puts p on the lines above whatever the composer already holds.
func (m *Model) withDraft(p queuedPrompt) queuedPrompt {
	text, imgs := m.parseComposer()
	if text == "" && len(imgs) == 0 {
		return p
	}
	return mergePrompts([]queuedPrompt{p, newQueuedPrompt(text, imgs)})
}

// restoreQueuedIntoComposer moves every queued follow-up into the composer,
// one per line, above any draft already there.
func (m *Model) restoreQueuedIntoComposer() bool {
	if len(m.queue.prompts) == 0 {
		return false
	}
	p := m.withDraft(mergePrompts(m.queue.prompts))
	m.queue.prompts = nil
	m.loadQueuedIntoComposer(p)
	m.composer.textarea.MoveToEnd()
	m.afterQueueChange()
	return true
}

// enqueuePrompt appends a waiting follow-up.
func (m *Model) enqueuePrompt(text string, imgs []image.Ref) tea.Cmd {
	m.queue.prompts = append(m.queue.prompts, newQueuedPrompt(text, imgs))
	m.resetInput()
	m.afterQueueChange()
	return nil
}

func (m *Model) afterQueueChange() {
	if m.term.ready {
		m.layoutPreservingBottom()
	}
}

// sendQueued submits the oldest queued follow-up as the next turn. Puts it
// back if submit refuses before committing.
func (m *Model) sendQueued() tea.Cmd {
	// OAuth recover owns the busy slot — do not start a competing turn.
	if m.session.AuthRetrying || len(m.queue.prompts) == 0 || m.turn.current != nil {
		return nil
	}
	p := m.queue.prompts[0]
	m.queue.prompts = m.queue.prompts[1:]
	m.afterQueueChange()
	return m.submitOrRequeue([]queuedPrompt{p})
}

// submitOrRequeue starts one turn from ps, each as its own user prompt; when
// submit refuses before committing, they go back to the front of the queue.
func (m *Model) submitOrRequeue(ps []queuedPrompt) tea.Cmd {
	last := ps[len(ps)-1]
	nHist := len(m.session.History)
	cmd := m.submitAfter(ps[:len(ps)-1], last.text, last.imgs)
	if len(m.session.History) == nHist {
		m.queue.prompts = append(append([]queuedPrompt(nil), ps...), m.queue.prompts...)
		m.afterQueueChange()
	}
	return cmd
}

// steerPrompt hands a message to the running turn.
func (m *Model) steerPrompt(text string, imgs []image.Ref) tea.Cmd {
	m.turn.current.steers.push(newQueuedPrompt(text, imgs))
	m.resetInput()
	m.afterQueueChange()
	return nil
}

// steersToQueue moves steers the loop has not taken to the front of the queue
// (they were sent before anything queued).
func (m *Model) steersToQueue() {
	if m.turn.current == nil {
		return
	}
	if left := m.turn.current.steers.drain(); len(left) > 0 {
		m.queue.prompts = append(left, m.queue.prompts...)
	}
}

// interruptWithSteers is Esc with steers pending: stop the turn and send the
// oldest steer right away as the next turn. Any others stay pending as steers
// of that turn (or queue up when it did not start); queued follow-ups keep
// waiting.
func (m *Model) interruptWithSteers() tea.Cmd {
	if m.session.AuthRetrying {
		return nil
	}
	steers := m.turn.current.steers.drain()
	first, rest := steers[0], steers[1:]
	m.finishTurn()
	nHist := len(m.session.History)
	cmd := m.submitOrRequeue([]queuedPrompt{first})
	switch {
	case len(rest) == 0:
	case m.turn.current != nil:
		for _, p := range rest {
			m.turn.current.steers.push(p)
		}
	case len(m.session.History) == nHist: // refused: first is back at the front
		m.queue.prompts = append(m.queue.prompts[:1:1], append(rest, m.queue.prompts[1:]...)...)
	default: // compacting first: they go ahead of the queue
		m.queue.prompts = append(rest, m.queue.prompts...)
	}
	m.afterQueueChange()
	return cmd
}

func (m *Model) composerIsEmpty() bool {
	text, imgs := m.parseComposer()
	return text == "" && len(imgs) == 0
}

// commitUserPrompt appends one user turn to transcript, API history, and JSONL.
func (m *Model) commitUserPrompt(text string, imgs []image.Ref) {
	m.transcript.messages = append(m.transcript.messages, Message{Role: RoleUser, Text: userDisplayText(text, imgs)})
	if err := m.session.CommitUserPrompt(text, imgs); err != nil {
		m.transcript.messages = append(m.transcript.messages, Message{Role: RoleError, Text: "session save failed: " + err.Error()})
	}
}

// restoreUnstartedPrompt rolls the last user turn back into the composer.
// Used when Esc/Ctrl+C abort a turn (or OAuth recover) before any model/tool
// output. Requires an empty composer so a typed follow-up is not overwritten,
// and the user row must still be the transcript tail (a later compact divider,
// agent/tool row, or persist error means the send already landed).
func (m *Model) restoreUnstartedPrompt() bool {
	if !m.composerIsEmpty() {
		return false
	}
	n := len(m.transcript.messages)
	if n == 0 || m.transcript.messages[n-1].Role != RoleUser {
		return false
	}
	nh := len(m.session.History)
	if nh == 0 || m.session.History[nh-1].Role != ai.RoleUser {
		return false
	}
	if m.session.Log != nil && m.session.Log.Persisted() {
		dropped, err := m.session.Log.DropLastUser()
		if err != nil || !dropped {
			return false
		}
	}
	h := m.session.History[nh-1]
	text := h.Text
	imgs := append([]image.Ref(nil), h.Images...)
	m.transcript.messages = m.transcript.messages[:n-1]
	m.session.History = m.session.History[:nh-1]
	m.loadQueuedIntoComposer(newQueuedPrompt(text, imgs))
	m.composer.textarea.MoveToEnd()
	m.refreshTranscript()
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
	m.composer.pendingImages = make(map[int]image.Ref, len(p.imgs))
	var b strings.Builder
	b.WriteString(p.text)
	for _, img := range p.imgs {
		m.composer.nextImageN++
		n := m.composer.nextImageN
		m.composer.pendingImages[n] = img
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(imageToken(n))
	}
	m.setPromptValue(b.String())
}

func (m *Model) handleTurnDone() tea.Cmd {
	// Late/spurious Done after cancel/error: do not drain remaining queue.
	if m.turn.current == nil {
		return nil
	}
	// A steer the loop never took (the turn ended first) goes out as the next
	// turn, ahead of anything queued.
	left := m.turn.current.steers.drain()
	m.finishTurn()
	if len(left) > 0 {
		return m.submitOrRequeue(left)
	}
	if cmd := m.sendQueued(); cmd != nil {
		return cmd
	}
	m.maybeOfferPlan()
	m.refreshTranscript()
	return nil
}

// renderQueueFollowups draws the follow-ups above the input: steers waiting
// for the next tool boundary, then queued follow-ups with their edit hint.
func (m *Model) renderQueueFollowups(width int) string {
	var steers []queuedPrompt
	if m.turn.current != nil {
		steers = m.turn.current.steers.snapshot()
	}
	queued := m.queue.prompts
	if len(steers) == 0 && len(queued) == 0 {
		return ""
	}
	dim := styles.FollowUpsHint
	w := max(4, width-2*styles.InputMarginH)
	var lines []string
	section := func(header string, ps []queuedPrompt, italic bool) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, header)
		st := lipgloss.NewStyle() // your own words at full weight
		if italic {
			st = dim.Italic(true)
		}
		for _, p := range ps {
			label := p.display
			if label == "" {
				label = "image"
			}
			var rows []string
			for _, l := range strings.Split(label, "\n") {
				rows = append(rows, strings.Split(ansi.Wrap(l, w-4, ""), "\n")...)
			}
			for i, r := range rows {
				if i == queueLineLimit {
					lines = append(lines, st.Render("    …"))
					break
				}
				prefix := "    "
				if i == 0 {
					prefix = "  ↳ "
				}
				lines = append(lines, st.Render(prefix+r))
			}
		}
	}
	bold := lipgloss.NewStyle().Bold(true)
	if len(steers) > 0 {
		head := bold.Render("Steering") + dim.Render(" · sent after the next tool call · ") +
			bold.Render("esc") + dim.Render(" interrupts and sends now")
		section(head, steers, false)
	}
	if len(queued) > 0 {
		head := bold.Render("Queued") + dim.Render(" · sent when the turn ends · ") +
			bold.Render("alt+↑/↓") + dim.Render(" edit")
		section(head, queued, true)
	}
	box := lipgloss.NewStyle().Margin(0, styles.InputMarginH).
		Render(strings.Join(lines, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, "", box)
}
