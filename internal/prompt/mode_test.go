package prompt

import (
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/workspace"
)

func TestModeNext(t *testing.T) {
	tests := []struct {
		in, want Mode
	}{
		{ModeBuild, ModeAsk},
		{ModeAsk, ModePlan},
		{ModePlan, ModeBuild},
	}
	for _, tt := range tests {
		if got := tt.in.Next(); got != tt.want {
			t.Errorf("%v.Next() = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestModeStringAndLabel(t *testing.T) {
	tests := []struct {
		mode       Mode
		wantString string
		wantLabel  string
	}{
		{ModeBuild, "build", "Build"},
		{ModeAsk, "ask", "Ask"},
		{ModePlan, "plan", "Plan"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.wantString {
			t.Errorf("%v.String() = %q, want %q", tt.mode, got, tt.wantString)
		}
		if got := tt.mode.Label(); got != tt.wantLabel {
			t.Errorf("%v.Label() = %q, want %q", tt.mode, got, tt.wantLabel)
		}
	}
}

func TestSystemExcludesMode(t *testing.T) {
	s := System(workspace.Context{})
	for _, needle := range []string{"<agent_mode>", "Mode: Build", "Mode: Ask", "Mode: Plan"} {
		if strings.Contains(s, needle) {
			t.Errorf("System() unexpectedly contains %q", needle)
		}
	}
}

func TestModeInstructions(t *testing.T) {
	for _, mode := range []Mode{ModeBuild, ModeAsk, ModePlan} {
		s := mode.Instructions()
		if !strings.HasPrefix(s, "\n<agent_mode>\n") {
			t.Errorf("%v.Instructions() missing leading newline and <agent_mode> open tag", mode)
		}
		if !strings.HasSuffix(s, "\n</agent_mode>\n") {
			t.Errorf("%v.Instructions() missing </agent_mode> close tag and trailing newline", mode)
		}
		if !strings.Contains(s, "Mode: "+mode.Label()) {
			t.Errorf("%v.Instructions() missing mode heading for %q", mode, mode.Label())
		}
	}
}
