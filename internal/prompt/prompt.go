package prompt

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/axispx/zeta/internal/skill"
	"github.com/axispx/zeta/internal/workspace"
)

//go:embed system.md
var systemMD string

// System returns the harness system prompt: identity, skill catalog, and
// project instructions. It stays byte-stable for the life of a session (it
// changes only when AGENTS.md does) because providers key their prompt cache
// on the request prefix — anything volatile here would invalidate the cached
// transcript every time it moved. Mode instructions are injected separately
// via Mode.Instructions(); volatile environment context via Environment.
func System(ws workspace.Context) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(systemMD))
	if cat := skill.Catalog(); cat != "" {
		b.WriteString("\n\n# Skills\n\n")
		b.WriteString(cat)
	}
	if ws.AgentsMD != "" {
		b.WriteString("\n\n# Project instructions\n\n")
		b.WriteString("Follow these project-specific instructions from AGENTS.md:\n\n")
		b.WriteString(ws.AgentsMD)
		b.WriteString("\n")
	}
	return b.String()
}

// Environment returns the current-environment block. The harness sends it as a
// developer message after the transcript: the date and branch change mid-session
// (midnight, a checkout) and restating the cwd is cheap, so keeping them out of
// System leaves the cached prefix — system prompt and history — untouched.
func Environment(ws workspace.Context) string {
	var b strings.Builder
	b.WriteString("# Environment\n\n")
	b.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("- Date: %s\n", time.Now().Format("2006-01-02")))
	if ws.Cwd != "" {
		b.WriteString(fmt.Sprintf("- Working directory: %s\n", ws.Cwd))
	}
	if ws.Branch != "" {
		b.WriteString(fmt.Sprintf("- Git branch: %s\n", ws.Branch))
	}
	return strings.TrimSuffix(b.String(), "\n")
}
