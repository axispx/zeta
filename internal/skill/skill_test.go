package skill

import (
	"strings"
	"testing"
)

func TestReviewSkillBundled(t *testing.T) {
	s, ok := Get("review")
	if !ok {
		t.Fatal("expected bundled review skill")
	}
	if s.Slash != "/review" {
		t.Fatalf("slash: %q", s.Slash)
	}
	if !strings.Contains(s.Content, "Thermo-Nuclear") {
		t.Fatalf("body missing title: %q", s.Content[:min(80, len(s.Content))])
	}
	got, ok := BySlash("/review")
	if !ok || got.Name != "review" {
		t.Fatalf("BySlash: %+v %v", got, ok)
	}
	cat := Catalog()
	if !strings.Contains(cat, "<name>review</name>") {
		t.Fatalf("catalog missing review: %s", cat)
	}
	if !strings.Contains(cat, s.Description) {
		t.Fatalf("catalog missing description: %s", cat)
	}
}

func TestGetMissing(t *testing.T) {
	if _, ok := Get("definitely-not-a-real-skill-name-zzz"); ok {
		t.Fatal("expected miss")
	}
}

func TestMatchSlash(t *testing.T) {
	tests := []struct {
		in   string
		ok   bool
		name string
	}{
		{"/review", true, "review"},
		{"  /review  ", true, "review"},
		{"/review focus on tui", true, "review"},
		{"/review\tfocus", true, "review"},
		{"/review-extra", false, ""},
		{"/re", false, ""},
		{"review", false, ""},
		{"", false, ""},
	}
	for _, tt := range tests {
		got, ok := MatchSlash(tt.in)
		if ok != tt.ok {
			t.Fatalf("MatchSlash(%q) ok=%v want %v", tt.in, ok, tt.ok)
		}
		if ok && got.Name != tt.name {
			t.Fatalf("MatchSlash(%q) name=%q want %q", tt.in, got.Name, tt.name)
		}
	}
}

func TestFormatContent(t *testing.T) {
	s, ok := Get("review")
	if !ok {
		t.Fatal("missing review")
	}
	got := FormatContent(s)
	if !strings.Contains(got, `<skill_content name="review">`) || !strings.Contains(got, "</skill_content>") {
		t.Fatalf("format: %s", got[:min(200, len(got))])
	}
	if !strings.Contains(got, s.Content) {
		t.Fatal("missing body")
	}
}

func TestSlashInjection(t *testing.T) {
	s, _ := Get("review")
	got := SlashInjection(s)
	if !strings.Contains(got, "<skill_content") || !strings.Contains(got, "Execute this skill now.") {
		t.Fatalf("injection: %s", got[:min(200, len(got))])
	}
	if !strings.Contains(got, "arguments in the user message") {
		t.Fatalf("injection missing args guidance: %s", got[len(got)-120:])
	}
}

func TestValidate(t *testing.T) {
	if _, err := validate(Skill{Name: "", Description: "d", Slash: "/x", Content: "body"}); err == nil {
		t.Fatal("expected name error")
	}
	if _, err := validate(Skill{Name: "x", Description: "", Slash: "/x", Content: "body"}); err == nil {
		t.Fatal("expected desc error")
	}
	if _, err := validate(Skill{Name: "x", Description: "d", Slash: "/x", Content: ""}); err == nil {
		t.Fatal("expected body error")
	}
	if _, err := validate(Skill{Name: "x", Description: "d", Slash: "nope", Content: "body"}); err == nil {
		t.Fatal("expected invalid slash error")
	}
	if _, err := validate(Skill{Name: "x", Description: "d", Slash: "/bad token", Content: "body"}); err == nil {
		t.Fatal("expected whitespace slash error")
	}
	// Tool-only (no slash) is fine. Harness collisions are enforced by tui.
	s, err := validate(Skill{Name: "ok", Description: "d", Content: "body"})
	if err != nil || s.Slash != "" {
		t.Fatalf("%+v %v", s, err)
	}
	s, err = validate(Skill{Name: "ok", Description: "d", Slash: "/ok", Content: "body"})
	if err != nil || s.Slash != "/ok" {
		t.Fatalf("%+v %v", s, err)
	}
	// skill package does not reserve harness tokens.
	if _, err := validate(Skill{Name: "x", Description: "d", Slash: "/clear", Content: "body"}); err != nil {
		t.Fatalf("shape-only validate should allow /clear: %v", err)
	}
}

func TestAllSorted(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("empty")
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Name > all[i].Name {
			t.Fatalf("unsorted: %q > %q", all[i-1].Name, all[i].Name)
		}
	}
}
