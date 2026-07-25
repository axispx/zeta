package prompt

import (
	"fmt"
	"strings"
)

// Mode controls how zeta responds to prompts.
type Mode int

const (
	ModeBuild Mode = iota
	ModePlan
	ModeAsk
)

var modeNames = [...]string{
	ModeBuild: "build",
	ModePlan:  "plan",
	ModeAsk:   "ask",
}

func (m Mode) valid() bool {
	return m >= 0 && int(m) < len(modeNames)
}

// String returns the lowercase mode name.
func (m Mode) String() string {
	if !m.valid() {
		return modeNames[ModeBuild]
	}
	return modeNames[m]
}

// Label returns the display name shown in the footer.
func (m Mode) Label() string {
	s := m.String()
	return strings.ToUpper(s[:1]) + s[1:]
}

// Next cycles Build → Plan → Ask → Build.
func (m Mode) Next() Mode {
	if !m.valid() {
		return ModeBuild
	}
	return Mode((int(m) + 1) % len(modeNames))
}

// Instructions returns the active mode prompt for a developer message,
// wrapped in <agent_mode> tags. The system prompt stays mode-agnostic; this
// fragment is injected separately and replaced when the mode changes.
func (m Mode) Instructions() string {
	var body string
	switch m {
	case ModeAsk:
		body = modeAskMD
	case ModePlan:
		body = modePlanMD
	default:
		body = modeBuildMD
	}
	return fmt.Sprintf("\n<agent_mode>\n%s\n</agent_mode>\n", strings.TrimSpace(body))
}
