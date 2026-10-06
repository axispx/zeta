package prompt

import (
	"fmt"
	"strings"
)

// Instructions returns the agent instructions for a developer message,
// wrapped in <agent_mode> tags. The system prompt stays instruction-agnostic;
// this fragment is injected separately.
func Instructions() string {
	return fmt.Sprintf("\n<agent_mode>\n%s\n</agent_mode>\n", strings.TrimSpace(modeBuildMD))
}
