package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/core"
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
	m.WS = workspace.Context{Abs: dir, Cwd: dir, AgentsMD: "Use tabs."}
	hist := []ai.Message{{Role: ai.RoleUser, Text: "go"}}
	before := textOf(core.RequestMsgs(m.WS, prompt.ModeBuild, hist, nil))

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use spaces."), 0o644); err != nil {
		t.Fatal(err)
	}
	m.RefreshWorkspace()
	if m.WS.AgentsMD != "Use tabs." {
		t.Fatalf("turn boundary reloaded AGENTS.md: %q", m.WS.AgentsMD)
	}
	if after := textOf(core.RequestMsgs(m.WS, prompt.ModeBuild, hist, nil)); !slices.Equal(before, after) {
		t.Fatalf("prefix changed mid-session:\n%q\n%q", before, after)
	}

	// A new session picks the edit up.
	m.startNewSession()
	if m.WS.AgentsMD != "Use spaces." {
		t.Fatalf("session boundary kept the old AGENTS.md: %q", m.WS.AgentsMD)
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
	m.WS = workspace.Context{Abs: dir, Cwd: dir, AgentsMD: "Use tabs."}
	m.History = []ai.Message{
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
	m.handleCompactDone(compactDoneMsg{kind: compactManual, result: compact.Result{History: m.History}})
	if m.WS.AgentsMD != "Use tabs." {
		t.Fatalf("no-op compaction reloaded AGENTS.md: %q", m.WS.AgentsMD)
	}

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use spaces."), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyCompactResult(compacted)

	if m.WS.AgentsMD != "Use spaces." {
		t.Fatalf("compaction kept the stale AGENTS.md: %q", m.WS.AgentsMD)
	}
	msgs := core.RequestMsgs(m.WS, prompt.ModeBuild, m.History, m.Todos)
	if !strings.Contains(msgs[0].Text, "Use spaces.") {
		t.Fatalf("compacted request missing the new AGENTS.md: %q", msgs[0].Text)
	}
	if !compact.IsCheckpoint(m.History[0]) {
		t.Fatalf("expected a checkpoint head, got %q", m.History[0].Text)
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
	m.WS = workspace.Context{Abs: dir, Cwd: dir, AgentsMD: "Use tabs."}
	m.History = []ai.Message{{Role: ai.RoleUser, Text: "latest"}}

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

	if m.WS.AgentsMD != "Use spaces." {
		t.Fatalf("auto-compact kept the stale AGENTS.md: %q", m.WS.AgentsMD)
	}
	if got := core.RequestMsgs(m.WS, prompt.ModeBuild, m.History, m.Todos)[0].Text; !strings.Contains(got, "Use spaces.") {
		t.Fatalf("auto-compacted request missing the new AGENTS.md: %q", got)
	}
}
