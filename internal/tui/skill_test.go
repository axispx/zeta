package tui

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/core"
	"github.com/axispx/zeta/internal/skill"
)

func TestSkillSlashDoesNotCollideWithBuiltins(t *testing.T) {
	builtins := make(map[string]struct{}, len(builtinCommands))
	for _, c := range builtinCommands {
		if c.skill {
			t.Fatalf("builtin %s marked skill", c.name)
		}
		builtins[c.name] = struct{}{}
	}
	for _, s := range skill.All() {
		if s.Slash == "" {
			continue
		}
		if _, ok := builtins[s.Slash]; ok {
			t.Fatalf("skill %q slash %q collides with a harness command", s.Name, s.Slash)
		}
	}
	// Palette must include both sets; skill flag set only on playbook rows.
	for _, c := range builtinCommands {
		got, ok := lookupCommand(c.name)
		if !ok || got.skill {
			t.Fatalf("builtin %s: ok=%v skill=%v", c.name, ok, got.skill)
		}
	}
	for _, s := range skill.All() {
		if s.Slash == "" {
			continue
		}
		got, ok := lookupCommand(s.Slash)
		if !ok || !got.skill {
			t.Fatalf("skill slash %s: ok=%v skill=%v", s.Slash, ok, got.skill)
		}
	}
}

func TestSubmitInputSkillWithArgs(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/review focus on stream.go")
	m := Model{textarea: ta, Session: core.Session{Cfg: testClientCfg()}}
	m.ApplyClient()
	cmd := m.submitInput()
	_ = cmd
	if len(m.History) != 1 || m.History[0].Text != "/review focus on stream.go" {
		t.Fatalf("history: %+v", m.History)
	}
	if len(m.messages) == 0 || m.messages[0].Role != RoleUser {
		t.Fatalf("messages: %+v", m.messages)
	}
}

func TestSubmitInputPaletteSkillFillsInput(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/rev")
	m := Model{textarea: ta}
	_ = m.syncOverlay()
	if !m.overlay.showing() {
		t.Fatal("expected command overlay")
	}
	// Select /review in the filtered list.
	for i, c := range m.overlay.cmds {
		if c.name == "/review" {
			m.overlay.selected = i
			break
		}
	}
	if m.overlay.cmds[m.overlay.selected].name != "/review" {
		t.Fatalf("selected = %#v", m.overlay.cmds)
	}
	// Palette Enter is handled by handleOverlayKey (not submitInput).
	cmd, ok := m.handleOverlayKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !ok {
		t.Fatal("enter should fill skill from palette")
	}
	if cmd != nil {
		t.Fatal("palette skill select should not start a turn")
	}
	if got := m.textarea.Value(); got != "/review " {
		t.Fatalf("input = %q, want %q", got, "/review ")
	}
	if m.overlay.showing() {
		t.Fatal("overlay should dismiss after fill")
	}
	if len(m.History) != 0 {
		t.Fatalf("should not submit: history=%+v", m.History)
	}
}

func TestSubmitInputExactSkillTokenFills(t *testing.T) {
	// Palette Enter always fills skills (never runs) so args can be added.
	ta := textarea.New()
	ta.SetValue("/review")
	m := Model{textarea: ta, Session: core.Session{Cfg: testClientCfg()}}
	m.ApplyClient()
	_ = m.syncOverlay()
	if !m.overlay.showing() {
		t.Fatal("expected command overlay for exact token")
	}
	cmd, ok := m.handleOverlayKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !ok {
		t.Fatal("enter should fill skill from palette")
	}
	if cmd != nil {
		t.Fatal("palette skill select should not start a turn")
	}
	if got := m.textarea.Value(); got != "/review " {
		t.Fatalf("input = %q, want %q", got, "/review ")
	}
	if m.overlay.showing() {
		t.Fatal("overlay should dismiss after fill")
	}
	if len(m.History) != 0 {
		t.Fatalf("should not submit: history=%+v", m.History)
	}
	// Second Enter (no overlay; trailing space trimmed) runs the skill turn.
	_ = m.submitInput()
	if len(m.History) != 1 || m.History[0].Text != "/review" {
		t.Fatalf("history after second enter: %+v", m.History)
	}
}

func TestTabSkillFillsInput(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/rev")
	m := Model{textarea: ta}
	_ = m.syncOverlay()
	for i, c := range m.overlay.cmds {
		if c.name == "/review" {
			m.overlay.selected = i
			break
		}
	}
	if _, ok := m.handleOverlayKey(tea.KeyPressMsg{Code: tea.KeyTab}); !ok {
		t.Fatal("tab should be consumed")
	}
	if got := m.textarea.Value(); got != "/review " {
		t.Fatalf("input = %q, want %q", got, "/review ")
	}
	if m.overlay.showing() {
		t.Fatal("overlay should dismiss after tab-fill")
	}
}
