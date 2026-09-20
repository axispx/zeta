package core

import (
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/prompt"
)

func TestProducedPlan(t *testing.T) {
	s := &Session{Mode: prompt.ModePlan}
	text := "Intro.\n\n<proposed_plan>\n## Ship\nDo it.\n</proposed_plan>\n"
	body, ok := s.ProducedPlan(text)
	if !ok || !strings.Contains(body, "Do it.") {
		t.Fatalf("body=%q ok=%v", body, ok)
	}

	// Markdown fence form (common model mistake).
	if body, ok := s.ProducedPlan("```proposed_plan\n## Fence\nbody here\n```"); !ok || !strings.Contains(body, "body here") {
		t.Fatalf("fence body=%q ok=%v", body, ok)
	}

	// No plan block, and non-plan modes, yield nothing.
	if _, ok := s.ProducedPlan("just prose"); ok {
		t.Fatal("prose must not produce a plan")
	}
	s.Mode = prompt.ModeBuild
	if _, ok := s.ProducedPlan(text); ok {
		t.Fatal("build mode must not produce a plan")
	}
}
