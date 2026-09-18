package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/prompt"
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
	first := textOf(requestMsgs(ws, prompt.ModeBuild, hist[:1], nil))
	later := textOf(requestMsgs(ws, prompt.ModeBuild, hist, nil))

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

	before := textOf(requestMsgs(main, prompt.ModeBuild, hist, nil))
	after := textOf(requestMsgs(feature, prompt.ModeBuild, hist, nil))

	if got, want := before[:len(before)-1], after[:len(after)-1]; !slices.Equal(got, want) {
		t.Fatalf("branch change rewrote the prefix:\n%q\n%q", got, want)
	}
	if before[len(before)-1] == after[len(after)-1] {
		t.Fatal("environment must report the new branch")
	}
}

// AGENTS.md is read once per session: a mid-session edit (including one the
// agent makes itself) must not change the request prefix, or every following
// turn would miss the provider cache.
func TestRefreshWorkspaceKeepsAgentsSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use tabs."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Trust(dir); err != nil {
		t.Fatal(err)
	}

	m := testModel()
	m.ws = workspace.Context{Abs: dir, Cwd: dir, AgentsMD: "Use tabs."}
	hist := []ai.Message{{Role: ai.RoleUser, Text: "go"}}
	before := textOf(requestMsgs(m.ws, prompt.ModeBuild, hist, nil))

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use spaces."), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshWorkspace()
	if m.ws.AgentsMD != "Use tabs." {
		t.Fatalf("turn boundary reloaded AGENTS.md: %q", m.ws.AgentsMD)
	}
	if after := textOf(requestMsgs(m.ws, prompt.ModeBuild, hist, nil)); !slices.Equal(before, after) {
		t.Fatalf("prefix changed mid-session:\n%q\n%q", before, after)
	}

	// A new session picks the edit up.
	m.startNewSession()
	if m.ws.AgentsMD != "Use spaces." {
		t.Fatalf("session boundary kept the old AGENTS.md: %q", m.ws.AgentsMD)
	}
}

// Compaction is a session boundary for project instructions: the conversation
// layer is rebuilt anyway, so an edited AGENTS.md is picked up there for
// roughly free. A compaction that changed nothing must not reload, since that
// would invalidate a prefix nothing was rebuilding.
func TestCompactReloadsAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use tabs."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Trust(dir); err != nil {
		t.Fatal(err)
	}

	m := testModel()
	m.ws = workspace.Context{Abs: dir, Cwd: dir, AgentsMD: "Use tabs."}
	m.history = []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("old ", 500)},
		{Role: ai.RoleAssistant, Text: "working"},
		{Role: ai.RoleUser, Text: "latest"},
	}
	compacted := compact.Result{
		History: []ai.Message{
			compact.CheckpointMessage("## Task\n- ship it"),
			{Role: ai.RoleUser, Text: "latest"},
		},
		Summary:   "## Task\n- ship it",
		TailCount: 1,
		Compacted: true,
	}

	// A no-op compaction leaves the snapshot (and so the prefix) alone.
	m.handleCompactDone(compactDoneMsg{kind: compactManual, result: compact.Result{History: m.history}})
	if m.ws.AgentsMD != "Use tabs." {
		t.Fatalf("no-op compaction reloaded AGENTS.md: %q", m.ws.AgentsMD)
	}

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use spaces."), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyCompactResult(compacted)

	if m.ws.AgentsMD != "Use spaces." {
		t.Fatalf("compaction kept the stale AGENTS.md: %q", m.ws.AgentsMD)
	}
	msgs := requestMsgs(m.ws, prompt.ModeBuild, m.history, m.todos)
	if !strings.Contains(msgs[0].Text, "Use spaces.") {
		t.Fatalf("compacted request missing the new AGENTS.md: %q", msgs[0].Text)
	}
	if !compact.IsCheckpoint(m.history[0]) {
		t.Fatalf("expected a checkpoint head, got %q", m.history[0].Text)
	}
}

// The auto path compacts, then immediately runs the user's turn. The reload
// from compaction must survive that path, and an edit landing mid-compaction
// must not leak the staleness check.
func TestAutoCompactKeepsReloadedAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use tabs."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Trust(dir); err != nil {
		t.Fatal(err)
	}

	m := testModel()
	m.ws = workspace.Context{Abs: dir, Cwd: dir, AgentsMD: "Use tabs."}
	m.history = []ai.Message{{Role: ai.RoleUser, Text: "latest"}}

	// The file changes while the compact request is in flight.
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use spaces."), 0o644); err != nil {
		t.Fatal(err)
	}
	m.handleCompactDone(compactDoneMsg{
		kind: compactAuto,
		result: compact.Result{
			History: []ai.Message{
				compact.CheckpointMessage("## Task\n- ship it"),
				{Role: ai.RoleUser, Text: "latest"},
			},
			Summary:   "## Task\n- ship it",
			TailCount: 1,
			Compacted: true,
		},
	})

	if m.ws.AgentsMD != "Use spaces." {
		t.Fatalf("auto-compact kept the stale AGENTS.md: %q", m.ws.AgentsMD)
	}
	if got := requestMsgs(m.ws, prompt.ModeBuild, m.history, m.todos)[0].Text; !strings.Contains(got, "Use spaces.") {
		t.Fatalf("auto-compacted request missing the new AGENTS.md: %q", got)
	}
}
