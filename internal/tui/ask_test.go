package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/agent"
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
	p.lists[0].selected = 2 // Other
	p.other[0] = "hybrid approach"
	resp := p.buildResponse()
	if resp.Answers["approach"] != "hybrid approach" {
		t.Fatalf("%v", resp.Answers["approach"])
	}
}

func TestHandleAskSubmitSendsResult(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		bottom: bottomSlot{ask: newAskPrompt(sampleAskArgs())},
		turn:   &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	// select second option
	m.bottom.ask.lists[0].selected = 1
	m.submitAsk()
	if m.bottom.ask != nil {
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
		bottom: bottomSlot{ask: newAskPrompt(sampleAskArgs())},
		turn:   &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	if _, ok := m.handleAskKey(tea.KeyPressMsg{Code: tea.KeyDown, Text: "down"}); !ok {
		t.Fatal("expected handled")
	}
	if m.bottom.ask.lists[0].selected != 1 {
		t.Fatalf("selected=%d", m.bottom.ask.lists[0].selected)
	}
	if _, ok := m.handleAskKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}); !ok {
		t.Fatal("enter")
	}
	r := <-replies
	if !strings.Contains(r.Result, "Full rewrite") {
		t.Fatalf("%s", r.Result)
	}
}

func TestHandleAskTypeJumpsToOther(t *testing.T) {
	m := Model{bottom: bottomSlot{ask: newAskPrompt(sampleAskArgs())}}
	m.bottom.ask.lists[0].selected = 2
	m.bottom.ask.typing = true
	m.bottom.ask.other[0] = "x"
	if !m.handleAskType(tea.KeyPressMsg{Code: tea.KeyBackspace, Text: "backspace"}) {
		t.Fatal("backspace")
	}
	if m.bottom.ask.other[0] != "" {
		t.Fatalf("other=%q", m.bottom.ask.other[0])
	}
}

func TestRenderAskShowsOptions(t *testing.T) {
	m := Model{width: 80, bottom: bottomSlot{ask: newAskPrompt(sampleAskArgs())}}
	out := stripANSI(m.renderAsk(80))
	if !strings.Contains(out, "Simple (Recommended)") {
		t.Fatalf("missing option: %q", out)
	}
	if !strings.Contains(out, "Other") {
		t.Fatalf("missing Other: %q", out)
	}
	if !strings.Contains(out, "How should we structure") {
		t.Fatalf("missing question: %q", out)
	}
}

func TestOpenAskFromToolStart(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{turn: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}}
	raw, _ := json.Marshal(sampleAskArgs())
	m.openAskFromToolStart(raw)
	if m.bottom.ask == nil || len(m.bottom.ask.questions) != 1 {
		t.Fatalf("%+v", m.bottom.ask)
	}
}

func TestOpenAskInvalidArgsReturnsErrorResult(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{turn: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}}
	m.openAskFromToolStart(json.RawMessage(`{"questions":[]}`))
	r := <-replies
	if r.Kind != agent.ReplyInject || !strings.Contains(r.Result, "error:") {
		t.Fatalf("%+v", r)
	}
}

func TestAbandonAskDenies(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		bottom: bottomSlot{ask: newAskPrompt(sampleAskArgs())},
		turn:   &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	m.abandonAsk()
	if m.bottom.ask != nil {
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
		bottom: bottomSlot{ask: newAskPrompt(args)},
		turn:   &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	m.submitAsk() // advance to q2
	if m.bottom.ask == nil || m.bottom.ask.qi != 1 {
		t.Fatalf("qi=%v ask=%v", m.bottom.ask, m.bottom.ask)
	}
	m.bottom.ask.lists[1].selected = 1
	m.submitAsk()
	r := <-replies
	var resp tools.AskUserResponse
	_ = json.Unmarshal([]byte(r.Result), &resp)
	if resp.Answers["q1"] != "A" || resp.Answers["q2"] != "D" {
		t.Fatalf("%s", r.Result)
	}
}
