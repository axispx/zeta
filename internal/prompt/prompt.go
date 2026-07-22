package prompt

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/axispx/zeta/internal/workspace"
)

//go:embed system.md
var systemMD string

// System returns the harness system prompt plus environment context.
// Mode instructions are injected separately via Mode.Instructions().
func System(ws workspace.Context) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(systemMD))
	b.WriteString("\n\n# Environment\n\n")
	b.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("- Date: %s\n", time.Now().Format("2006-01-02")))
	if ws.Cwd != "" {
		b.WriteString(fmt.Sprintf("- Working directory: %s\n", ws.Cwd))
	}
	if ws.Branch != "" {
		b.WriteString(fmt.Sprintf("- Git branch: %s\n", ws.Branch))
	}
	if ws.AgentsMD != "" {
		b.WriteString("\n# Project instructions\n\n")
		b.WriteString("Follow these project-specific instructions from AGENTS.md:\n\n")
		b.WriteString(ws.AgentsMD)
		b.WriteString("\n")
	}
	return b.String()
}
