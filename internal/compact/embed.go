package compact

import (
	_ "embed"
	"strings"
)

//go:embed prompts/system.md
var summarizerSystemMD string

//go:embed prompts/template.md
var summaryTemplateMD string

func summarizerSystem() string {
	return strings.TrimSpace(summarizerSystemMD)
}

func summaryTemplate() string {
	return strings.TrimSpace(summaryTemplateMD)
}
