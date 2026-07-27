package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/skill"
	"github.com/axispx/zeta/internal/workspace"
)

func TestRequestMsgsExpandsSlashSkill(t *testing.T) {
	hist := []ai.Message{{Role: ai.RoleUser, Text: "/review"}}
	msgs := requestMsgs(workspace.Context{}, prompt.ModeBuild, hist, nil)
	if len(msgs) < 3 {
		t.Fatalf("len=%d", len(msgs))
	}
	// system, mode developer, user /review, skill developer
	var sawUser, sawSkill bool
	for _, m := range msgs {
		if m.Role == ai.RoleUser && m.Text == "/review" {
			sawUser = true
		}
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "Thermo-Nuclear") {
			sawSkill = true
		}
	}
	if !sawUser || !sawSkill {
		t.Fatalf("missing expand: user=%v skill=%v msgs=%+v", sawUser, sawSkill, rolesOf(msgs))
	}
	// Durable hist unchanged.
	if len(hist) != 1 || hist[0].Text != "/review" {
		t.Fatalf("hist mutated: %+v", hist)
	}
}

func TestRequestMsgsExpandsSlashSkillWithArgs(t *testing.T) {
	const user = "/review focus on tui packaging"
	hist := []ai.Message{{Role: ai.RoleUser, Text: user}}
	msgs := requestMsgs(workspace.Context{}, prompt.ModeBuild, hist, nil)
	var sawUser, sawSkill bool
	for _, m := range msgs {
		if m.Role == ai.RoleUser && m.Text == user {
			sawUser = true
		}
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "Thermo-Nuclear") {
			sawSkill = true
			if !strings.Contains(m.Text, "arguments in the user message") {
				t.Fatalf("injection missing args guidance")
			}
		}
	}
	if !sawUser || !sawSkill {
		t.Fatalf("missing expand with args: user=%v skill=%v", sawUser, sawSkill)
	}
	if len(hist) != 1 || hist[0].Text != user {
		t.Fatalf("hist mutated: %+v", hist)
	}
}

func TestRequestMsgsNoReinjectCompletedSlash(t *testing.T) {
	for _, user := range []string{"/review", "/review focus on tui"} {
		hist := []ai.Message{
			{Role: ai.RoleUser, Text: user},
			{Role: ai.RoleAssistant, Text: "done"},
		}
		msgs := requestMsgs(workspace.Context{}, prompt.ModeBuild, hist, nil)
		for _, m := range msgs {
			if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "Thermo-Nuclear") {
				t.Fatalf("re-injected completed slash %q: %+v", user, rolesOf(msgs))
			}
		}
	}
}

func TestRequestMsgsNoopPlainUser(t *testing.T) {
	hist := []ai.Message{{Role: ai.RoleUser, Text: "no slash"}}
	msgs := requestMsgs(workspace.Context{}, prompt.ModeBuild, hist, nil)
	for _, m := range msgs {
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "skill_content") {
			t.Fatalf("unexpected skill inject: %+v", m)
		}
	}
}

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
	m := Model{textarea: ta}
	// No client → submit still records the user turn then errors.
	cmd := m.submitInput()
	_ = cmd
	if len(m.history) != 1 || m.history[0].Text != "/review focus on stream.go" {
		t.Fatalf("history: %+v", m.history)
	}
	if len(m.messages) == 0 || m.messages[0].Role != RoleUser {
		t.Fatalf("messages: %+v", m.messages)
	}
}

func TestSubmitInputPaletteSkillFillsInput(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/rev")
	m := Model{textarea: ta}
	m.syncOverlay()
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
	cmd := m.submitInput()
	if cmd != nil {
		t.Fatal("palette skill select should not start a turn")
	}
	if got := m.textarea.Value(); got != "/review " {
		t.Fatalf("input = %q, want %q", got, "/review ")
	}
	if m.overlay.showing() {
		t.Fatal("overlay should dismiss after fill")
	}
	if len(m.history) != 0 {
		t.Fatalf("should not submit: history=%+v", m.history)
	}
}

func TestSubmitInputExactSkillTokenFills(t *testing.T) {
	// Palette Enter always fills skills (never runs) so args can be added.
	ta := textarea.New()
	ta.SetValue("/review")
	m := Model{textarea: ta}
	m.syncOverlay()
	if !m.overlay.showing() {
		t.Fatal("expected command overlay for exact token")
	}
	if cmd := m.submitInput(); cmd != nil {
		t.Fatal("palette skill select should not start a turn")
	}
	if got := m.textarea.Value(); got != "/review " {
		t.Fatalf("input = %q, want %q", got, "/review ")
	}
	if m.overlay.showing() {
		t.Fatal("overlay should dismiss after fill")
	}
	if len(m.history) != 0 {
		t.Fatalf("should not submit: history=%+v", m.history)
	}
	// Second Enter (no overlay; trailing space trimmed) runs the skill turn.
	_ = m.submitInput()
	if len(m.history) != 1 || m.history[0].Text != "/review" {
		t.Fatalf("history after second enter: %+v", m.history)
	}
}

func TestTabSkillFillsInput(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/rev")
	m := Model{textarea: ta}
	m.syncOverlay()
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

func rolesOf(msgs []ai.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = string(m.Role)
	}
	return out
}
