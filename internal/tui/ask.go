package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/tools"
)

const (
	// askOtherLabel is the freeform row's placeholder text, replaced by the
	// typed answer.
	askOtherLabel = "Type an answer"
	// askOtherAnswer is the model-facing answer when the freeform row is
	// chosen with nothing typed. The row is the UI's own, so the model knows it
	// as "Other" and never sees the placeholder phrasing.
	askOtherAnswer = "Other"
)

// askPrompt is the panel for tools.AskUser.
// Replaces the input until the user answers or the turn is cancelled.
type askPrompt struct {
	questions []tools.AskQuestion
	qi        int // current question index
	// one optionList per question (includes synthetic Other as last row)
	lists []optionList
	// freeform text when Other is chosen
	other []string
	// typing is true while the freeform field owns key input
	typing bool
}

func newAskPrompt(args tools.AskUserArgs) *askPrompt {
	n := len(args.Questions)
	p := &askPrompt{
		questions: args.Questions,
		lists:     make([]optionList, n),
		other:     make([]string, n),
	}
	for i := range args.Questions {
		p.lists[i].setRows(askRows(args.Questions[i]))
	}
	return p
}

func askRows(q tools.AskQuestion) []optionRow {
	labels := make([]string, 0, len(q.Options)+1)
	hints := make([]string, 0, len(q.Options)+1)
	for _, o := range q.Options {
		labels = append(labels, o.Label)
		hints = append(hints, o.Description)
	}
	// Other carries no description: typing replaces its label instead.
	labels = append(labels, askOtherLabel)
	hints = append(hints, "")
	return numberedRows(labels, hints)
}

// syncOther turns the freeform row into the input field: its label is the typed
// answer (or the placeholder while empty) with a caret while it owns the keys.
// Derived state — call it before rendering or hit-testing, the only readers.
func (p *askPrompt) syncOther() {
	list := p.curList()
	q, ok := p.current()
	if !ok || list == nil {
		return
	}
	i := len(q.Options)
	if i < 0 || i >= list.n() {
		return
	}
	text := p.other[p.qi]
	r := &list.rows[i]
	r.hint, r.labelCursor, r.label = "", p.typing, askOtherLabel
	if strings.TrimSpace(text) != "" {
		r.label = text
	}
}

func (p *askPrompt) current() (tools.AskQuestion, bool) {
	if p == nil || p.qi < 0 || p.qi >= len(p.questions) {
		return tools.AskQuestion{}, false
	}
	return p.questions[p.qi], true
}

func (p *askPrompt) curList() *optionList {
	if p == nil || p.qi < 0 || p.qi >= len(p.lists) {
		return nil
	}
	return &p.lists[p.qi]
}

func (p *askPrompt) optionCount(qi int) int {
	if p == nil || qi < 0 || qi >= len(p.lists) {
		return 0
	}
	return p.lists[qi].n()
}

func (p *askPrompt) isOther(qi int) bool {
	if p == nil || qi < 0 || qi >= len(p.lists) || qi >= len(p.questions) {
		return false
	}
	return p.lists[qi].selected == len(p.questions[qi].Options)
}

// buildResponse maps selections to the model-facing JSON payload.
// UI is single-select: each question emits exactly one answer string.
func (p *askPrompt) buildResponse() tools.AskUserResponse {
	out := tools.AskUserResponse{Answers: make(map[string]string, len(p.questions))}
	for i, q := range p.questions {
		var answer string
		if p.isOther(i) {
			if t := strings.TrimSpace(p.other[i]); t != "" {
				answer = t
			} else {
				answer = askOtherAnswer
			}
		} else {
			oi := p.lists[i].selected
			if oi >= 0 && oi < len(q.Options) {
				answer = q.Options[oi].Label
			}
		}
		out.Answers[q.ID] = answer
	}
	return out
}

func (m *Model) abandonAsk() {
	if m.panel.ask == nil {
		return
	}
	m.sendReply(agent.DenyTool())
	m.panel.clear()
	m.afterPanelChange()
}

func (m *Model) submitAsk() {
	p := m.panel.ask
	if p == nil {
		return
	}
	// If Other is selected with empty text on the current question, focus typing.
	if p.isOther(p.qi) && strings.TrimSpace(p.other[p.qi]) == "" {
		p.typing = true
		m.afterPanelChange()
		return
	}
	// Multi-question: advance until last.
	if p.qi < len(p.questions)-1 {
		p.qi++
		p.typing = p.isOther(p.qi)
		m.afterPanelChange()
		return
	}
	m.sendReply(agent.InjectResult(tools.FormatAskUserResponse(p.buildResponse())))
	m.panel.clear()
	m.afterPanelChange()
}

// handleAskKey consumes keys while the ask panel is open.
// Esc returns handled=false so Update's interrupt path still runs.
func (m *Model) handleAskKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.panel.ask
	if p == nil {
		return nil, false
	}
	key := msg.String()
	if key == "esc" {
		return nil, false
	}

	// Freeform field owns printable input / backspace when typing or Other focused.
	if p.typing || (p.isOther(p.qi) && isAskTextKey(msg)) {
		p.typing = true
		return nil, m.handleAskType(msg)
	}

	list := p.curList()
	if list == nil {
		return nil, true
	}

	// Digit jump into Other / options without treating as freeform text.
	if idx := digitOption(key, list.n()); idx >= 0 {
		list.selected = idx
		p.typing = p.isOther(p.qi)
		return nil, true
	}

	// Typing while options focused jumps into Other freeform.
	if isAskTextKey(msg) {
		list.selected = len(p.questions[p.qi].Options) // Other
		p.typing = true
		return nil, m.handleAskType(msg)
	}

	switch key {
	case "left", "shift+tab":
		if p.qi > 0 {
			p.qi--
			p.typing = p.isOther(p.qi)
		}
		return nil, true
	case "right", "tab":
		if p.isOther(p.qi) && !p.typing {
			p.typing = true
			return nil, true
		}
		if p.qi < len(p.questions)-1 {
			p.qi++
			p.typing = p.isOther(p.qi)
		}
		return nil, true
	case "enter":
		if p.isOther(p.qi) && strings.TrimSpace(p.other[p.qi]) == "" && !p.typing {
			p.typing = true
			return nil, true
		}
		m.submitAsk()
		return nil, true
	case "up", "ctrl+p", "down", "ctrl+n":
		_, _, handled := list.handleKey(msg)
		p.typing = false
		return nil, handled
	default:
		// Swallow remaining keys (list.handleKey would swallow too).
		return nil, true
	}
}

func isAskTextKey(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "backspace", "ctrl+h", "ctrl+w", "ctrl+u":
		return true
	}
	return askText(msg) != ""
}

// askText is the printable text a key press contributes, "" for named keys.
// Read msg.Text rather than msg.String(): the space bar's keystroke is "space"
// while its text is " ", and a bracketed paste arrives as one multi-rune text.
func askText(msg tea.KeyPressMsg) string {
	if msg.Text != "" && isPrintable(msg.Text) {
		return msg.Text
	}
	// Synthetic key presses carry only Code.
	if s := msg.String(); len(s) == 1 && unicode.IsPrint(rune(s[0])) {
		return s
	}
	return ""
}

// isPrintable reports whether every rune in s is printable, so control text
// (an enter's "\r", a tab's "\t") is never treated as input.
func isPrintable(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func (m *Model) handleAskType(msg tea.KeyPressMsg) bool {
	p := m.panel.ask
	if p == nil {
		return false
	}
	// Ensure Other is selected while typing.
	if list := p.curList(); list != nil {
		list.selected = len(p.questions[p.qi].Options)
	}
	cur := p.other[p.qi]
	switch msg.String() {
	case "enter":
		m.submitAsk()
	case "esc":
		return false
	case "backspace", "ctrl+h":
		if cur != "" {
			r := []rune(cur)
			p.other[p.qi] = string(r[:len(r)-1])
		} else {
			p.typing = false
		}
	case "ctrl+u":
		p.other[p.qi] = ""
	case "ctrl+w":
		p.other[p.qi] = trimLastWord(cur)
	case "up", "ctrl+p", "down", "ctrl+n", "tab", "shift+tab":
		p.typing = false
	default:
		if t := askText(msg); t != "" {
			p.other[p.qi] = cur + t
		}
	}
	return true
}

func trimLastWord(s string) string {
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	if s == "" {
		return ""
	}
	r := []rune(s)
	i := len(r) - 1
	for i >= 0 && !unicode.IsSpace(r[i]) {
		i--
	}
	for i >= 0 && unicode.IsSpace(r[i]) {
		i--
	}
	if i < 0 {
		return ""
	}
	return string(r[:i+1])
}

// handleAskClick selects an option under the cursor.
func (m *Model) handleAskClick(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	p := m.panel.ask
	if p == nil || msg.Button != tea.MouseLeft {
		return nil, false
	}
	list := p.curList()
	if list == nil {
		return nil, false
	}
	p.syncOther()
	_, contentW := overlayWidths(m.term.width)
	idx, chose := list.handleClick(msg.X, msg.Y, m.transcript.viewport.Height(), m.term.width, m.askTitleH(), contentW)
	if !chose {
		return nil, false
	}
	_ = idx
	p.typing = p.isOther(p.qi)
	return nil, true
}

func (m *Model) handleAskMotion(msg tea.MouseMotionMsg) bool {
	p := m.panel.ask
	if p == nil {
		return false
	}
	list := p.curList()
	if list == nil {
		return false
	}
	p.syncOther()
	_, contentW := overlayWidths(m.term.width)
	return list.handleMotion(msg.X, msg.Y, m.transcript.viewport.Height(), m.term.width, m.askTitleH(), contentW)
}

func (m Model) askTitleH() int {
	_, contentW := overlayWidths(m.term.width)
	ink := m.term.chrome.OverlayInk()
	return lipgloss.Height(m.renderAskHeader(contentW, ink))
}

func (m Model) renderAsk(width int) string {
	p := m.panel.ask
	if p == nil {
		return ""
	}
	if _, ok := p.current(); !ok {
		return ""
	}
	p.syncOther()
	list := p.curList()
	if list == nil {
		return ""
	}
	_, contentW := overlayWidths(width)
	ink := m.term.chrome.OverlayInk()

	var b strings.Builder
	b.WriteString(m.renderAskHeader(contentW, ink))
	b.WriteString(list.render(contentW, ink))

	// Blank row between the options and the key hints.
	b.WriteByte('\n')
	b.WriteString(padPanel(ink.Gap.Width(panelInner(contentW)).Render(""), panelGutter))
	b.WriteByte('\n')
	b.WriteString(m.renderAskFooter(contentW, ink))

	return renderPanelFrame(m.term.chrome, width, b.String())
}

// panelInner is the row width inside a panel, excluding the gutter.
func panelInner(contentW int) int {
	return max(1, contentW-panelGutter)
}

func (m Model) renderAskHeader(contentW int, ink styles.OverlayInk) string {
	p := m.panel.ask
	if p == nil {
		return ""
	}
	q, ok := p.current()
	if !ok {
		return ""
	}
	inner := panelInner(contentW)
	var progress string
	if n := len(p.questions); n > 1 {
		progress = fmt.Sprintf("Question %d/%d · ", p.qi+1, n)
	}
	header := strings.TrimSpace(q.Header)
	if header == "" {
		header = "Question"
	}
	line1 := ink.Header.Render(progress) + ink.Header.Render(truncateRight(header, max(1, inner-len(progress))))
	body := ink.Gap.Width(inner).Render(wrapSimple(q.Question, inner))
	out := padPanel(ink.Gap.Width(inner).Render(line1), panelGutter)
	out += "\n" + padPanel(body, panelGutter)
	// Blank line between the question and its options.
	out += "\n" + padPanel(ink.Gap.Width(inner).Render(""), panelGutter)
	return out
}

func (m Model) renderAskFooter(contentW int, ink styles.OverlayInk) string {
	inner := panelInner(contentW)
	p := m.panel.ask
	var parts []string
	if p.typing {
		parts = append(parts, "enter submit")
	} else {
		parts = append(parts, "↑/↓ select", "enter submit")
		if p.isOther(p.qi) {
			parts = append(parts, "tab type answer")
		}
	}
	if len(p.questions) > 1 {
		parts = append(parts, "←/→ question")
	}
	parts = append(parts, "esc cancel")
	hint := strings.Join(parts, " · ")
	return padPanel(ink.HintText.Width(inner).Render(truncateRight(hint, inner)), panelGutter)
}

// wrapSimple hard-wraps s to width runes (space-aware when possible).
func wrapSimple(s string, width int) string {
	s = strings.TrimSpace(s)
	if width < 8 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	var b strings.Builder
	for len(runes) > 0 {
		if len(runes) <= width {
			b.WriteString(string(runes))
			break
		}
		chunk := runes[:width]
		cut := -1
		for i := len(chunk) - 1; i >= width/3; i-- {
			if unicode.IsSpace(chunk[i]) {
				cut = i
				break
			}
		}
		if cut < 0 {
			cut = width
		}
		b.WriteString(strings.TrimRightFunc(string(runes[:cut]), unicode.IsSpace))
		b.WriteByte('\n')
		runes = []rune(strings.TrimLeftFunc(string(runes[cut:]), unicode.IsSpace))
	}
	return b.String()
}

// openAskFromToolStart parses ask_user args and opens the panel.
func (m *Model) openAskFromToolStart(argsJSON json.RawMessage) {
	args, err := tools.ParseAskUserArgs(argsJSON)
	if err != nil {
		// Invalid args: inject error result so the model can recover.
		m.sendReply(agent.InjectResult("error: " + err.Error()))
		return
	}
	m.panel.setAsk(newAskPrompt(args))
	m.afterPanelChange()
}
