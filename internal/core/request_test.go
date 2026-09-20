package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/todo"
	"github.com/axispx/zeta/internal/workspace"
)

// textOf renders messages as "role|text" so non-comparable ai.Message values
// can be compared element-wise.
func textOf(msgs []ai.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = string(m.Role) + "|" + m.Text
	}
	return out
}

func rolesOf(msgs []ai.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = string(m.Role)
	}
	return out
}

// The provider caches the request prefix: system + mode + durable history must
// be byte-identical across turns, and growing history must extend that prefix
// rather than rewrite it. Only the trailing developer blocks may change.
func TestRequestMsgsKeepsCachedPrefix(t *testing.T) {
	ws := workspace.Context{Abs: "/p", Cwd: "~/p", Branch: "main", AgentsMD: "Use tabs."}
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: "first"},
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "second"},
	}
	first := textOf(RequestMsgs(ws, prompt.ModeBuild, hist[:1], nil))
	later := textOf(RequestMsgs(ws, prompt.ModeBuild, hist, nil))

	head := len(hist) + 2 // system + mode + history
	if got, want := later[2:head], textOf(hist); !slices.Equal(got, want) {
		t.Fatalf("history not in place: got %q want %q", got, want)
	}
	// The prefix shared by both turns: system prompt and mode block.
	if !slices.Equal(first[:2], later[:2]) {
		t.Fatalf("prefix changed between turns:\nfirst=%q\nlater=%q", first[:2], later[:2])
	}
	if first[2] != later[2] {
		t.Fatalf("history prefix rewritten: %q vs %q", first[2], later[2])
	}
	// Environment trails the transcript so a branch/cwd refresh cannot
	// invalidate the cached prefix.
	if last := later[len(later)-1]; !strings.HasPrefix(last, "developer|# Environment") {
		t.Fatalf("environment must trail the transcript: %q", last)
	}
}

// A checkout mid-session must leave the cached prefix untouched: only the
// trailing environment block may differ.
func TestRequestMsgsEnvironmentTrailsBranchChange(t *testing.T) {
	main := workspace.Context{Cwd: "~/p", Branch: "main", AgentsMD: "Use tabs."}
	feature := main
	feature.Branch = "feature"
	hist := []ai.Message{{Role: ai.RoleUser, Text: "go"}}

	before := textOf(RequestMsgs(main, prompt.ModeBuild, hist, nil))
	after := textOf(RequestMsgs(feature, prompt.ModeBuild, hist, nil))

	if got, want := before[:len(before)-1], after[:len(after)-1]; !slices.Equal(got, want) {
		t.Fatalf("branch change rewrote the prefix:\n%q\n%q", got, want)
	}
	if before[len(before)-1] == after[len(after)-1] {
		t.Fatal("environment must report the new branch")
	}
}

func TestRequestMsgsTodosBlock(t *testing.T) {
	hist := []ai.Message{{Role: ai.RoleUser, Text: "go"}}
	// empty store → no block
	empty := todo.NewStore()
	msgs := RequestMsgs(workspace.Context{}, prompt.ModeBuild, hist, empty)
	for _, m := range msgs {
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "# Session todos") {
			t.Fatalf("unexpected todos block: %q", m.Text)
		}
	}

	store := todo.NewStore()
	if _, err := store.Replace([]todo.Item{
		{ID: "1", Subject: "A", Status: todo.Pending},
	}); err != nil {
		t.Fatal(err)
	}
	msgs = RequestMsgs(workspace.Context{}, prompt.ModeBuild, hist, store)
	var saw, sawEnv bool
	// should trail history so the stable prefix stays cacheable
	envIdx, modeIdx, todoIdx, userIdx := -1, -1, -1, -1
	for i, m := range msgs {
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "# Mode: Build") {
			modeIdx = i
		}
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "# Environment") {
			envIdx = i
			sawEnv = true
		}
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "# Session todos") {
			todoIdx = i
			saw = true
			if !strings.Contains(m.Text, "- [ ] **1**: A") {
				t.Fatalf("block=%q", m.Text)
			}
		}
		if m.Role == ai.RoleUser {
			userIdx = i
		}
	}
	// mode + history keep their order; environment and todos trail the request
	// so the system/history prefix stays byte-stable for provider prompt
	// caching, with the most volatile block last.
	if !saw || !sawEnv || modeIdx != 1 || userIdx != modeIdx+1 || envIdx != userIdx+1 || todoIdx != len(msgs)-1 {
		t.Fatalf("order mode=%d env=%d todo=%d user=%d saw=%v/%v roles=%v",
			modeIdx, envIdx, todoIdx, userIdx, saw, sawEnv, rolesOf(msgs))
	}
}

func TestRequestMsgsExpandsSlashSkill(t *testing.T) {
	hist := []ai.Message{{Role: ai.RoleUser, Text: "/review"}}
	msgs := RequestMsgs(workspace.Context{}, prompt.ModeBuild, hist, nil)
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
	msgs := RequestMsgs(workspace.Context{}, prompt.ModeBuild, hist, nil)
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
		msgs := RequestMsgs(workspace.Context{}, prompt.ModeBuild, hist, nil)
		for _, m := range msgs {
			if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "Thermo-Nuclear") {
				t.Fatalf("re-injected completed slash %q: %+v", user, rolesOf(msgs))
			}
		}
	}
}

func TestRequestMsgsNoopPlainUser(t *testing.T) {
	hist := []ai.Message{{Role: ai.RoleUser, Text: "no slash"}}
	msgs := RequestMsgs(workspace.Context{}, prompt.ModeBuild, hist, nil)
	for _, m := range msgs {
		if m.Role == ai.RoleDeveloper && strings.Contains(m.Text, "skill_content") {
			t.Fatalf("unexpected skill inject: %+v", m)
		}
	}
}
