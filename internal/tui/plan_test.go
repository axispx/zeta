package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/workspace"
)

func TestMaybeOfferPlan(t *testing.T) {
	m := testModel()
	m.mode = prompt.ModePlan
	m.pendingPlan = "## Auth fix\nDo the thing."
	m.maybeOfferPlan()
	if m.bottom.plan == nil {
		t.Fatal("expected plan prompt")
	}
	if m.bottom.plan.title != "Auth fix" {
		t.Fatalf("title = %q", m.bottom.plan.title)
	}
	if !strings.Contains(m.bottom.plan.body, "Do the thing") {
		t.Fatalf("body = %q", m.bottom.plan.body)
	}
	if m.pendingPlan != "" {
		t.Fatal("pending should clear after offer")
	}
}

func TestMaybeOfferPlanSkipsWithoutPending(t *testing.T) {
	m := testModel()
	m.mode = prompt.ModePlan
	m.maybeOfferPlan()
	if m.bottom.plan != nil {
		t.Fatal("unexpected plan prompt")
	}
}

func TestMaybeOfferPlanSkipsBuildMode(t *testing.T) {
	m := testModel()
	m.mode = prompt.ModeBuild
	m.pendingPlan = "## X\nbody"
	m.maybeOfferPlan()
	if m.bottom.plan != nil {
		t.Fatal("build mode must not offer plan")
	}
	// pending is consumed even when skipped by mode
	if m.pendingPlan != "" {
		t.Fatal("pending should clear")
	}
}

func TestMaybeOfferPlanDoesNotRescanHistory(t *testing.T) {
	// After discard, history may still contain a plan; without pending, no re-offer.
	m := testModel()
	m.mode = prompt.ModePlan
	m.messages = []Message{{
		Role: RoleAgent,
		Text: "Intro.\n\n<proposed_plan>\n## Old\nstale\n</proposed_plan>",
	}}
	m.maybeOfferPlan()
	if m.bottom.plan != nil {
		t.Fatal("must not re-offer from history")
	}
}

func TestNoteProducedPlan(t *testing.T) {
	m := testModel()
	m.mode = prompt.ModePlan
	m.noteProducedPlan("Intro.\n\n<proposed_plan>\n## Ship\nDo it.\n</proposed_plan>\n")
	if m.pendingPlan == "" || !strings.Contains(m.pendingPlan, "Do it.") {
		t.Fatalf("pending=%q", m.pendingPlan)
	}
	// Markdown fence form (common model mistake).
	m.pendingPlan = ""
	m.noteProducedPlan("```proposed_plan\n## Fence plan\nbody here\n```")
	if m.pendingPlan == "" || !strings.Contains(m.pendingPlan, "body here") {
		t.Fatalf("fence pending=%q", m.pendingPlan)
	}
	m.mode = prompt.ModeBuild
	m.pendingPlan = ""
	m.noteProducedPlan("<proposed_plan>\n## X\ny\n</proposed_plan>")
	if m.pendingPlan != "" {
		t.Fatal("build mode must not note plan")
	}
}

func TestLoadSessionKeepsPlanTagsOnAgent(t *testing.T) {
	text := "Here.\n\n<proposed_plan>\n## T\nbody\n</proposed_plan>"
	ui, hist := loadSession([]session.Record{
		{Role: session.RoleUser, Text: "plan please"},
		{Role: session.RoleAgent, Text: text},
	})
	if len(ui) != 2 || ui[1].Role != RoleAgent || ui[1].Text != text {
		t.Fatalf("ui=%+v", ui)
	}
	if len(hist) != 2 || hist[1].Text != text {
		t.Fatalf("hist=%+v", hist)
	}
}

func TestHandlePlanKeyApproveOpensBuildPick(t *testing.T) {
	m := testModel()
	m.cfg = sampleTUIConfig()
	m.mode = prompt.ModePlan
	m.bottom.plan = newPlanPrompt("## T\nbody", "T")

	cmd, ok := m.handlePlanKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !ok {
		t.Fatal("expected handled")
	}
	if cmd != nil {
		t.Fatal("approve should only open picker")
	}
	if m.bottom.plan != nil {
		t.Fatalf("plan approval should clear, plan=%+v", m.bottom.plan)
	}
	if m.bottom.build == nil || len(m.bottom.build.models) == 0 {
		t.Fatalf("expected build pick panel, build=%+v", m.bottom.build)
	}
	if m.overlay.mode != overlayOff {
		t.Fatalf("must not use shared overlay, mode=%v", m.overlay.mode)
	}
	if !m.inputBlocked() {
		t.Fatal("input should stay hidden while picking build model")
	}
}

func TestHandlePlanKeyRevise(t *testing.T) {
	m := testModel()
	m.bottom.plan = newPlanPrompt("x", "T")
	m.pendingPlan = "should clear"
	cmd, ok := m.handlePlanKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if !ok {
		t.Fatal("expected handled")
	}
	if cmd != nil {
		t.Fatal("revise should not auto-start a turn")
	}
	if m.bottom.plan != nil {
		t.Fatal("plan panel should clear")
	}
	if m.pendingPlan != "" {
		t.Fatal("pending should clear on dismiss")
	}
	if n := len(m.messages); n == 0 || !strings.Contains(m.messages[n-1].Text, "Revise") {
		t.Fatalf("expected revise note, messages=%+v", m.messages)
	}
}

func TestHandlePlanKeyDiscard(t *testing.T) {
	m := testModel()
	m.bottom.plan = newPlanPrompt("x", "T")
	_, ok := m.handlePlanKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if !ok {
		t.Fatal("expected handled")
	}
	if m.bottom.plan != nil {
		t.Fatal("plan should clear")
	}
}

func TestTryInterruptDismissesPlan(t *testing.T) {
	m := testModel()
	m.bottom.plan = newPlanPrompt("x", "T")
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.bottom.plan != nil {
		t.Fatal("plan still open")
	}
}

func TestTryInterruptBuildPickBackToPlan(t *testing.T) {
	m := testModel()
	m.bottom.build = &buildPickPrompt{
		body:   "x",
		title:  "T",
		models: []config.ModelChoice{{ProviderID: "p", ModelID: "m", Name: "M"}},
	}
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.bottom.build != nil {
		t.Fatal("build pick should clear")
	}
	if m.bottom.plan == nil || m.bottom.plan.body != "x" {
		t.Fatalf("should return to approval, plan=%+v", m.bottom.plan)
	}
}

func TestConfirmPlanBuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)

	m := testModel()
	m.cfg = sampleTUIConfig()
	m.ws = workspace.Context{Abs: home, Cwd: home}
	m.mode = prompt.ModePlan
	m.bottom.build = &buildPickPrompt{
		body:  "## Ship it\n\n1. Do work",
		title: "Ship it",
	}
	m.client = nil

	_ = m.confirmPlanBuild("deepseek/deepseek-v4-flash")
	if m.mode != prompt.ModeBuild {
		t.Fatalf("mode = %v", m.mode)
	}
	if m.bottom.plan != nil || m.bottom.build != nil {
		t.Fatal("plan/build should clear")
	}
	if m.cfg.Active != "deepseek/deepseek-v4-flash" {
		t.Fatalf("active = %q", m.cfg.Active)
	}
	if m.cfg.Defaults.Build != "deepseek/deepseek-v4-flash" {
		t.Fatalf("build default = %q", m.cfg.Defaults.Build)
	}

	found := false
	for _, msg := range m.history {
		if strings.Contains(msg.Text, "Ship it") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("history missing plan: %+v", m.history)
	}
}

func sampleTUIConfig() config.Config {
	return config.Config{
		Active: "deepseek/deepseek-v4-flash",
		Providers: map[string]config.Provider{
			"deepseek": {
				Name:    "DeepSeek",
				BaseURL: "https://api.deepseek.com/v1",
				APIKey:  "sk-test",
				Models: map[string]config.ModelDef{
					"deepseek-v4-flash": {Name: "V4 Flash", ContextWindow: 128000},
					"deepseek-chat":     {Name: "Chat", ContextWindow: 64000},
				},
			},
		},
	}
}
