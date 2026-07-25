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
	askOtherLabel       = "Other"
	askOtherDescription = "Type a custom answer"
	askOtherPlaceholder = "Your answer…"
)

// askPrompt is the bottom panel for tools.AskUser.
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
	labels = append(labels, askOtherLabel)
	hints = append(hints, askOtherDescription)
	return numberedRows(labels, hints)
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
				answer = askOtherLabel
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
	if m.bottom.ask == nil {
		return
	}
	m.sendReply(agent.DenyTool())
	m.bottom.clear()
	m.afterSetBottom()
}

func (m *Model) submitAsk() {
	p := m.bottom.ask
	if p == nil {
		return
	}
	// If Other is selected with empty text on the current question, focus typing.
	if p.isOther(p.qi) && strings.TrimSpace(p.other[p.qi]) == "" {
		p.typing = true
		m.afterSetBottom()
		return
	}
	// Multi-question: advance until last.
	if p.qi < len(p.questions)-1 {
		p.qi++
		p.typing = p.isOther(p.qi)
		m.afterSetBottom()
		return
	}
	m.sendReply(agent.InjectResult(tools.FormatAskUserResponse(p.buildResponse())))
	m.bottom.clear()
	m.afterSetBottom()
}

// handleAskKey consumes keys while the ask panel is open.
// Esc returns handled=false so Update's interrupt path still runs.
func (m *Model) handleAskKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.bottom.ask
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
	s := msg.String()
	if s == "backspace" || s == "ctrl+h" || s == "ctrl+w" || s == "ctrl+u" {
		return true
	}
	if len(s) != 1 {
		return false
	}
	r := rune(s[0])
	return unicode.IsPrint(r) && !unicode.IsControl(r)
}

func (m *Model) handleAskType(msg tea.KeyPressMsg) bool {
	p := m.bottom.ask
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
		s := msg.String()
		if len(s) == 1 {
			r := rune(s[0])
			if unicode.IsPrint(r) {
				p.other[p.qi] = cur + string(r)
			}
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
	p := m.bottom.ask
	if p == nil || msg.Button != tea.MouseLeft {
		return nil, false
	}
	list := p.curList()
	if list == nil {
		return nil, false
	}
	idx, chose := list.handleClick(msg.X, msg.Y, m.viewport.Height(), m.width, m.askTitleH())
	if !chose {
		return nil, false
	}
	_ = idx
	p.typing = p.isOther(p.qi)
	return nil, true
}

func (m *Model) handleAskMotion(msg tea.MouseMotionMsg) bool {
	p := m.bottom.ask
	if p == nil {
		return false
	}
	list := p.curList()
	if list == nil {
		return false
	}
	return list.handleMotion(msg.X, msg.Y, m.viewport.Height(), m.width, m.askTitleH())
}

func (m Model) askTitleH() int {
	_, contentW := overlayWidths(m.width)
	ink := m.chrome.OverlayInk()
	return lipgloss.Height(m.renderAskHeader(contentW, ink))
}

func (m Model) renderAsk(width int) string {
	p := m.bottom.ask
	if p == nil {
		return ""
	}
	if _, ok := p.current(); !ok {
		return ""
	}
	list := p.curList()
	if list == nil {
		return ""
	}
	_, contentW := overlayWidths(width)
	ink := m.chrome.OverlayInk()

	var b strings.Builder
	b.WriteString(m.renderAskHeader(contentW, ink))
	b.WriteString(list.render(contentW, ink))

	if p.isOther(p.qi) {
		b.WriteByte('\n')
		b.WriteString(m.renderAskOtherField(contentW, ink))
	}

	b.WriteByte('\n')
	b.WriteString(m.renderAskFooter(contentW, ink))

	return renderBottomPanel(m.chrome, width, b.String())
}

func (m Model) renderAskHeader(contentW int, ink styles.OverlayInk) string {
	p := m.bottom.ask
	if p == nil {
		return ""
	}
	q, ok := p.current()
	if !ok {
		return ""
	}
	inner := contentW - panelGutter
	if inner < 1 {
		inner = 1
	}
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
	return out
}

func (m Model) renderAskOtherField(contentW int, ink styles.OverlayInk) string {
	p := m.bottom.ask
	inner := contentW - panelGutter
	if inner < 1 {
		inner = 1
	}
	text := p.other[p.qi]
	var line string
	if p.typing {
		if text == "" {
			line = ink.Kbd.Render("› ") + ink.HintText.Render(askOtherPlaceholder)
		} else {
			line = ink.Kbd.Render("› ") + ink.Gap.Render(truncateRight(text, max(1, inner-2))) + ink.Kbd.Render("█")
		}
	} else {
		if text == "" {
			line = ink.HintText.Render("  " + askOtherPlaceholder)
		} else {
			line = ink.Gap.Render("  " + truncateRight(text, max(1, inner-2)))
		}
	}
	return padPanel(ink.Gap.Width(inner).Render(line), panelGutter)
}

func (m Model) renderAskFooter(contentW int, ink styles.OverlayInk) string {
	inner := contentW - panelGutter
	if inner < 1 {
		inner = 1
	}
	p := m.bottom.ask
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
	m.bottom.setAsk(newAskPrompt(args))
	m.afterSetBottom()
}
