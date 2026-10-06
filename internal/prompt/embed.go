// Package prompt holds the system prompt and agent instructions.
package prompt

import _ "embed"

//go:embed modes/build.md
var modeBuildMD string
