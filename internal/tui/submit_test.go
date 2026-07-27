package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
)

// testClientCfg is a provider good enough to build a client. The base URL is
// unroutable: tests that submit never run the command they get back.
func testClientCfg() config.Config {
	return config.Config{
		Active: "x/y",
		Providers: map[string]config.Provider{
			"x": {
				BaseURL: "http://127.0.0.1:1",
				APIKey:  "k",
				Models:  map[string]config.ModelDef{"y": {ContextWindow: 128000}},
			},
		},
	}
}

// A prompt that never leaves the machine must not join the API transcript,
// and the text must stay in the composer so it can be sent once a provider
// is configured.
func TestSubmitWithoutClientLeavesTurnUncommitted(t *testing.T) {
	m := testModel()
	m.textarea.SetValue("hey")

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	m = next.(Model)

	if len(m.history) != 0 {
		t.Fatalf("history = %#v", m.history)
	}
	if m.textarea.Value() != "hey" {
		t.Fatalf("input = %q", m.textarea.Value())
	}
	if len(m.messages) != 1 {
		t.Fatalf("messages = %#v", m.messages)
	}
	if m.messages[0].Role != RoleError {
		t.Fatalf("role = %v", m.messages[0].Role)
	}
	if !strings.Contains(m.messages[0].Text, "/config") {
		t.Fatalf("text = %q", m.messages[0].Text)
	}
}
