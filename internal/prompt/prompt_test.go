package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/axispx/zeta/internal/workspace"
)

// System is the head of the request prefix providers cache, so it must not
// carry anything that changes mid-session.
func TestSystemCarriesNoVolatileContext(t *testing.T) {
	s := System(workspace.Context{
		Cwd:      "~/proj",
		Branch:   "main",
		AgentsMD: "Use tabs.",
	})
	for _, needle := range []string{"# Environment", "Git branch", "Working directory",
		time.Now().Format("2006-01-02")} {
		if strings.Contains(s, needle) {
			t.Errorf("System() contains volatile %q", needle)
		}
	}
}

// A checkout, a cwd change, or a day rollover must leave System byte-identical
// and land only in the trailing Environment block.
func TestSystemStableAcrossWorkspaceChanges(t *testing.T) {
	base := workspace.Context{Cwd: "~/proj", Branch: "main", AgentsMD: "Use tabs."}
	moved := workspace.Context{Cwd: "~/other", Branch: "feature", AgentsMD: "Use tabs."}
	if System(base) != System(moved) {
		t.Fatal("System() changed with cwd/branch")
	}
	if Environment(base) == Environment(moved) {
		t.Fatal("Environment() must report the new cwd/branch")
	}
}

func TestEnvironmentFields(t *testing.T) {
	s := Environment(workspace.Context{Cwd: "~/proj", Branch: "main"})
	for _, needle := range []string{"# Environment", "- OS: ", "- Date: ", "- Working directory: ~/proj", "- Git branch: main"} {
		if !strings.Contains(s, needle) {
			t.Errorf("Environment() missing %q: %q", needle, s)
		}
	}
	// Optional fields stay omitted rather than rendering empty lines.
	if bare := Environment(workspace.Context{}); strings.Contains(bare, "Working directory") || strings.Contains(bare, "Git branch") {
		t.Errorf("Environment() should omit empty cwd/branch: %q", bare)
	}
}
