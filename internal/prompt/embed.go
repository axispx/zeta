// Package prompt holds the system prompt and per-mode instructions.
package prompt

import _ "embed"

//go:embed modes/build.md
var modeBuildMD string

//go:embed modes/plan.md
var modePlanMD string
