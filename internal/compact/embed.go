package compact

import (
	_ "embed"
	"strings"
)

//go:embed prompts/instruction.md
var summarizerInstructionMD string

//go:embed prompts/template.md
var summaryTemplateMD string

func summarizerInstruction() string {
	return strings.TrimSpace(summarizerInstructionMD)
}

func summaryTemplate() string {
	return strings.TrimSpace(summaryTemplateMD)
}
