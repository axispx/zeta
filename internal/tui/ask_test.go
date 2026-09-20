package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/tools"
)

func sampleAskArgs() tools.AskUserArgs {
	return tools.AskUserArgs{
		Questions: []tools.AskQuestion{
			{
				ID:       "approach",
				Header:   "Approach",
				Question: "How should we structure this?",
				Options: []tools.AskOption{
					{Label: "Simple (Recommended)", Description: "Minimal change."},
					{Label: "Full rewrite", Description: "Bigger blast radius."},
				},
			},
		},
	}
}

func TestAskPromptBuildResponseDefault(t *testing.T) {
	p := newAskPrompt(sampleAskArgs())
	resp := p.buildResponse()
	if got := resp.Answers["approach"]; got != "Simple (Recommended)" {
		t.Fatalf("%v", got)
	}
}

func TestAskPromptOtherFreeform(t *testing.T) {
	p := newAskPrompt(sampleAskArgs())
	p.lists[0].selected = 2 // freeform row
	p.other[0] = "hybrid approach"
	resp := p.buildResponse()
	if resp.Answers["approach"] != "hybrid approach" {
		t.Fatalf("%v", resp.Answers["approach"])
	}
}

// The row's placeholder is UI phrasing; an empty freeform answer still reaches
// the model as "Other", the name its tool description tells it to expect.
func TestAskPromptFreeformEmptyAnswersOther(t *testing.T) {
	p := newAskPrompt(sampleAskArgs())
	p.lists[0].selected = 2
	resp := p.buildResponse()
	if got := resp.Answers["approach"]; got != askOtherAnswer {
		t.Fatalf("answer = %q, want %q", got, askOtherAnswer)
	}
	if askOtherLabel == askOtherAnswer {
		t.Fatal("placeholder text must not be the model-facing answer")
	}
}

func TestHandleAskSubmitSendsResult(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		panel: panel{ask: newAskPrompt(sampleAskArgs())},
		turn:  turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	// select second option
	m.panel.ask.lists[0].selected = 1
	m.submitAsk()
	if m.panel.ask != nil {
		t.Fatal("ask should clear")
	}
	r := <-replies
	if r.Kind != agent.ReplyInject || r.Result == "" {
		t.Fatalf("%+v", r)
	}
	var resp tools.AskUserResponse
	if err := json.Unmarshal([]byte(r.Result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Answers["approach"] != "Full rewrite" {
		t.Fatalf("%s", r.Result)
	}
}

func TestHandleAskKeyNavAndEnter(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		panel: panel{ask: newAskPrompt(sampleAskArgs())},
		turn:  turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	if _, ok := m.handleAskKey(tea.KeyPressMsg{Code: tea.KeyDown}); !ok {
		t.Fatal("expected handled")
	}
	if m.panel.ask.lists[0].selected != 1 {
		t.Fatalf("selected=%d", m.panel.ask.lists[0].selected)
	}
	if _, ok := m.handleAskKey(tea.KeyPressMsg{Code: tea.KeyEnter}); !ok {
		t.Fatal("enter")
	}
	r := <-replies
	if !strings.Contains(r.Result, "Full rewrite") {
		t.Fatalf("%s", r.Result)
	}
}

func TestHandleAskTypeJumpsToOther(t *testing.T) {
	m := Model{panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	m.panel.ask.lists[0].selected = 2
	m.panel.ask.typing = true
	m.panel.ask.other[0] = "x"
	if !m.handleAskType(tea.KeyPressMsg{Code: tea.KeyBackspace}) {
		t.Fatal("backspace")
	}
	if m.panel.ask.other[0] != "" {
		t.Fatalf("other=%q", m.panel.ask.other[0])
	}
}

// spaceKey is a real space-bar press: text " " but keystroke "space".
func spaceKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
}

func TestAskTextKeySpace(t *testing.T) {
	if got := askText(spaceKey()); got != " " {
		t.Fatalf("space text = %q, want %q", got, " ")
	}
	if !isAskTextKey(spaceKey()) {
		t.Fatal("space must count as a text key")
	}
	// Named keys carry no text on the wire; control text is never input.
	for _, k := range []tea.KeyPressMsg{
		{Code: tea.KeyEnter},
		{Code: tea.KeyUp},
		{Code: tea.KeyTab},
		{Code: 'a', Mod: tea.ModCtrl},
		{Code: tea.KeyEnter, Text: "\r"},
	} {
		if got := askText(k); got != "" {
			t.Fatalf("askText(%q) = %q, want empty", k.String(), got)
		}
	}
}

func TestHandleAskTypeInsertsSpace(t *testing.T) {
	m := Model{panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	m.panel.ask.lists[0].selected = 2
	m.panel.ask.typing = true
	m.panel.ask.other[0] = "hybrid"
	m.handleAskType(spaceKey())
	m.handleAskType(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if got, want := m.panel.ask.other[0], "hybrid a"; got != want {
		t.Fatalf("other = %q, want %q", got, want)
	}
}

// A space on an option row drops into Other, like any other printable key.
func TestHandleAskKeySpaceJumpsToOther(t *testing.T) {
	m := Model{panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	m.panel.ask.lists[0].selected = 0
	if _, ok := m.handleAskKey(spaceKey()); !ok {
		t.Fatal("expected handled")
	}
	p := m.panel.ask
	if !p.typing || !p.isOther(p.qi) {
		t.Fatalf("typing=%v other=%v", p.typing, p.isOther(p.qi))
	}
	if p.other[0] != " " {
		t.Fatalf("other = %q, want a space", p.other[0])
	}
}

// A multi-rune text (bracketed paste) lands whole rather than being dropped.
func TestHandleAskTypePastesText(t *testing.T) {
	m := Model{panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	m.panel.ask.lists[0].selected = 2
	m.panel.ask.typing = true
	m.handleAskType(tea.KeyPressMsg{Code: tea.KeyExtended, Text: "two words"})
	if got, want := m.panel.ask.other[0], "two words"; got != want {
		t.Fatalf("other = %q, want %q", got, want)
	}
}

func TestRenderAskShowsOptions(t *testing.T) {
	m := Model{term: term{width: 80}, panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	out := stripANSI(m.renderAsk(80))
	if !strings.Contains(out, "Simple (Recommended)") {
		t.Fatalf("missing option: %q", out)
	}
	if !strings.Contains(out, askOtherLabel) {
		t.Fatalf("missing freeform row: %q", out)
	}
	if !strings.Contains(out, "How should we structure") {
		t.Fatalf("missing question: %q", out)
	}
}

// The question block ends with a blank row separating it from the options.
func TestRenderAskBlankAfterQuestion(t *testing.T) {
	m := Model{term: term{width: 80}, panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	lines := strings.Split(stripANSI(m.renderAsk(80)), "\n")
	i := -1
	for j, l := range lines {
		if strings.Contains(l, "How should we structure") {
			i = j
			break
		}
	}
	if i < 0 {
		t.Fatalf("missing question: %q", lines)
	}
	if strings.TrimSpace(lines[i+1]) != "" {
		t.Fatalf("line below question must be blank: %q", lines[i+1])
	}
	if !strings.Contains(lines[i+2], "Simple (Recommended)") {
		t.Fatalf("options must follow the gap: %q", lines[i+2])
	}
}

// The key hints sit one blank row below the last option.
func TestRenderAskGapAboveFooter(t *testing.T) {
	m := Model{term: term{width: 80}, panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	lines := strings.Split(stripANSI(m.renderAsk(80)), "\n")
	i := -1
	for j, l := range lines {
		if strings.Contains(l, "esc cancel") {
			i = j
			break
		}
	}
	if i < 1 {
		t.Fatalf("missing footer: %q", lines)
	}
	if strings.TrimSpace(lines[i-1]) != "" {
		t.Fatalf("line above footer must be blank: %q", lines[i-1])
	}
	if !strings.Contains(lines[i-2], askOtherLabel) {
		t.Fatalf("options must end above the gap: %q", lines[i-2])
	}
}

// Typing in the freeform row replaces its option text: no description line, caret at the end.
func TestRenderAskOtherReplacesLabel(t *testing.T) {
	m := Model{term: term{width: 80}, panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	p := m.panel.ask
	p.lists[0].selected = 2

	idle := stripANSI(m.renderAsk(80))
	if !strings.Contains(idle, "3. "+askOtherLabel) {
		t.Fatalf("freeform row must keep its number: %q", idle)
	}
	if strings.Contains(idle, askOtherLabel+optionCaret) {
		t.Fatalf("no caret while the field is idle: %q", idle)
	}
	idleLines := strings.Count(idle, "\n")

	p.typing = true
	blank := stripANSI(m.renderAsk(80))
	if !strings.Contains(blank, askOtherLabel+optionCaret) {
		t.Fatalf("empty field must show the caret on the placeholder: %q", blank)
	}

	p.other[0] = "hybrid approach"
	out := stripANSI(m.renderAsk(80))
	if !strings.Contains(out, "3. hybrid approach"+optionCaret) {
		t.Fatalf("answer must replace the option text: %q", out)
	}
	if strings.Contains(out, askOtherLabel) {
		t.Fatalf("replaced label must not linger: %q", out)
	}
	if n := strings.Count(out, "\n"); n != idleLines {
		t.Fatalf("typing must not change the panel height: %d → %d\n%s", idleLines, n, out)
	}
}

// A long answer scrolls from the left so the caret and newest keys stay visible.
func TestRenderAskOtherLabelScrolls(t *testing.T) {
	m := Model{term: term{width: 60}, panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	p := m.panel.ask
	p.lists[0].selected = 2
	p.typing = true
	p.other[0] = strings.Repeat("word ", 20) + "end"

	out := stripANSI(m.renderAsk(60))
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, optionCaret) {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "→ 3. …") {
			t.Fatalf("answer must scroll from the left: %q", line)
		}
		if !strings.HasSuffix(strings.TrimRight(line, " "), "end"+optionCaret) {
			t.Fatalf("answer must end at the caret: %q", line)
		}
		if lipgloss.Width(line) > 60 {
			t.Fatalf("row overflows: %d cols: %q", lipgloss.Width(line), line)
		}
		return
	}
	t.Fatalf("missing caret line: %q", out)
}

// A saved answer survives leaving the field: no caret once keys move off it.
func TestRenderAskOtherAnswerUnfocused(t *testing.T) {
	m := Model{term: term{width: 80}, panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	p := m.panel.ask
	p.other[0] = "hybrid approach"
	p.lists[0].selected = 2
	p.typing = false
	out := stripANSI(m.renderAsk(80))
	if !strings.Contains(out, "3. hybrid approach") {
		t.Fatalf("the chosen answer must stay visible: %q", out)
	}
	if strings.Contains(out, optionCaret) {
		t.Fatalf("caret must not show when the field is unfocused: %q", out)
	}
}

// Descriptions render under their option, so a click on one selects that row.
func TestHandleAskClickOnDescriptionLine(t *testing.T) {
	m := Model{term: term{width: 100}, panel: panel{ask: newAskPrompt(sampleAskArgs())}}
	m.panel.ask.lists[0].selected = 0
	titleH := m.askTitleH()
	// Rows are label · description, so row 1's description is line 3.
	y := m.transcript.viewport.Height() + 2 + titleH + 3
	if _, ok := m.handleAskClick(tea.MouseClickMsg{X: styles.InputMarginH + 1, Y: y, Button: tea.MouseLeft}); !ok {
		t.Fatal("expected click handled")
	}
	if m.panel.ask.lists[0].selected != 1 {
		t.Fatalf("selected=%d", m.panel.ask.lists[0].selected)
	}
}

func TestOpenAskFromToolStart(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{turn: turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}}}
	raw, _ := json.Marshal(sampleAskArgs())
	m.openAskFromToolStart(raw)
	if m.panel.ask == nil || len(m.panel.ask.questions) != 1 {
		t.Fatalf("%+v", m.panel.ask)
	}
}

func TestOpenAskInvalidArgsReturnsErrorResult(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{turn: turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}}}
	m.openAskFromToolStart(json.RawMessage(`{"questions":[]}`))
	r := <-replies
	if r.Kind != agent.ReplyInject || !strings.Contains(r.Result, "error:") {
		t.Fatalf("%+v", r)
	}
}

func TestAbandonAskDenies(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		panel: panel{ask: newAskPrompt(sampleAskArgs())},
		turn:  turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	m.abandonAsk()
	if m.panel.ask != nil {
		t.Fatal("cleared")
	}
	r := <-replies
	if r.Kind != agent.ReplyDeny {
		t.Fatal("expected deny")
	}
}

func TestMultiQuestionAdvance(t *testing.T) {
	args := tools.AskUserArgs{
		Questions: []tools.AskQuestion{
			{
				ID: "q1", Header: "One", Question: "First?",
				Options: []tools.AskOption{{Label: "A", Description: "a"}, {Label: "B", Description: "b"}},
			},
			{
				ID: "q2", Header: "Two", Question: "Second?",
				Options: []tools.AskOption{{Label: "C", Description: "c"}, {Label: "D", Description: "d"}},
			},
		},
	}
	replies := make(chan agent.Reply, 1)
	m := Model{
		panel: panel{ask: newAskPrompt(args)},
		turn:  turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	m.submitAsk() // advance to q2
	if m.panel.ask == nil || m.panel.ask.qi != 1 {
		t.Fatalf("qi=%v ask=%v", m.panel.ask, m.panel.ask)
	}
	m.panel.ask.lists[1].selected = 1
	m.submitAsk()
	r := <-replies
	var resp tools.AskUserResponse
	_ = json.Unmarshal([]byte(r.Result), &resp)
	if resp.Answers["q1"] != "A" || resp.Answers["q2"] != "D" {
		t.Fatalf("%s", r.Result)
	}
}
