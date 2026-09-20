package core

import (
	"github.com/axispx/zeta/internal/plan"
	"github.com/axispx/zeta/internal/prompt"
)

// ProducedPlan returns the plan body an assistant segment produced, when the
// session is in Plan mode. The caller owns the UI that offers it.
func (s *Session) ProducedPlan(asstText string) (string, bool) {
	if s.Mode != prompt.ModePlan {
		return "", false
	}
	return plan.Extract(asstText)
}
