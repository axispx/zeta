package plan

import (
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"empty", "", "", false},
		{"no tag", "just text", "", false},
		{
			"simple",
			"intro\n<proposed_plan>\n## Title\nbody\n</proposed_plan>\noutro",
			"## Title\nbody",
			true,
		},
		{
			"last wins",
			"<proposed_plan>old</proposed_plan>\n<proposed_plan>new plan</proposed_plan>",
			"new plan",
			true,
		},
		{"empty body", "<proposed_plan>\n\n</proposed_plan>", "", false},
		{"unclosed", "x <proposed_plan>no close", "", false},
		{
			"unclosed then closed",
			"<proposed_plan>broken\n<proposed_plan>ok</proposed_plan>",
			"ok",
			true,
		},
		{
			// Models often emit ```proposed_plan instead of <proposed_plan>.
			"markdown fence",
			"intro\n```proposed_plan\n## Fence\nbody\n```\noutro",
			"## Fence\nbody",
			true,
		},
		{
			"fence last wins over earlier tag",
			"<proposed_plan>old</proposed_plan>\n```proposed_plan\nnew fence\n```",
			"new fence",
			true,
		},
		{
			"tag last wins over earlier fence",
			"```proposed_plan\nold fence\n```\n<proposed_plan>new tag</proposed_plan>",
			"new tag",
			true,
		},
		{"fence empty", "```proposed_plan\n\n```", "", false},
		{"fence unclosed", "x\n```proposed_plan\nno close", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Extract(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("Extract() = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestTitle(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "Untitled plan"},
		{"## Fix auth\n\nDetails", "Fix auth"},
		{"# Heading\nbody", "Heading"},
		{"plain first line\nsecond", "plain first line"},
		{"###   spaced  ", "spaced"},
	}
	for _, tt := range tests {
		if got := Title(tt.in); got != tt.want {
			t.Errorf("Title(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildPrompt(t *testing.T) {
	got := BuildPrompt("## Do thing")
	if !strings.HasPrefix(got, "Implement") {
		t.Fatalf("unexpected prompt: %q", got)
	}
	if !strings.Contains(got, "## Do thing") {
		t.Fatalf("missing body: %q", got)
	}
}

func TestDisplayParts(t *testing.T) {
	before, body, after, ok := DisplayParts("Intro.\n\n<proposed_plan>\n## T\nbody\n</proposed_plan>\nOutro.")
	if !ok {
		t.Fatal("expected ok")
	}
	if before != "Intro.\n" && before != "Intro." {
		// TrimSuffix only strips one trailing newline after TrimRight spaces.
		if !strings.HasPrefix(before, "Intro.") {
			t.Fatalf("before=%q", before)
		}
	}
	if body != "## T\nbody" {
		t.Fatalf("body=%q", body)
	}
	if strings.TrimSpace(after) != "Outro." {
		t.Fatalf("after=%q", after)
	}
	if strings.Contains(before, "proposed_plan") || strings.Contains(body, "proposed_plan") || strings.Contains(after, "proposed_plan") {
		t.Fatal("tags must not appear in parts")
	}

	_, body, _, ok = DisplayParts("x\n<proposed_plan>\npartial")
	if !ok || body != "partial" {
		t.Fatalf("unclosed: body=%q ok=%v", body, ok)
	}
	if !Open("x\n<proposed_plan>\npartial") {
		t.Fatal("expected Open")
	}
	if Open("x\n<proposed_plan>y</proposed_plan>") {
		t.Fatal("closed should not be Open")
	}

	// Markdown fence form (model mistake).
	before, body, after, ok = DisplayParts("Hi.\n\n```proposed_plan\n## F\nb\n```\nBye.")
	if !ok {
		t.Fatal("fence: expected ok")
	}
	if body != "## F\nb" {
		t.Fatalf("fence body=%q", body)
	}
	if strings.Contains(before+body+after, "```") || strings.Contains(before+body+after, "proposed_plan") {
		t.Fatalf("fence delimiters leaked: before=%q body=%q after=%q", before, body, after)
	}
	if strings.TrimSpace(after) != "Bye." {
		t.Fatalf("fence after=%q", after)
	}
	if !Open("x\n```proposed_plan\npartial") {
		t.Fatal("expected fence Open")
	}
	if Open("x\n```proposed_plan\ny\n```") {
		t.Fatal("closed fence should not be Open")
	}
}
