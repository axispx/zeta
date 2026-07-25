// Package skill loads first-party playbooks embedded in the zeta binary.
// Skills are curated by zeta — no user/repo skill dirs.
package skill

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
)

// Skill is one bundled playbook.
type Skill struct {
	Name        string
	Description string
	Content     string // markdown body
	// Slash is the palette token (e.g. "/review"). Empty means tool-only.
	Slash string
}

//go:embed skills/review/SKILL.md
var reviewMD string

// Bundled playbooks. Metadata lives here; bodies are embedded markdown.
// Add a skill: embed the file, append a row. Rebuild picks it up.
var bundled = []Skill{
	{
		Name:        "review",
		Description: "Strict code-quality review of the current branch",
		Slash:       "/review",
		Content:     reviewMD,
	},
}

var (
	loaded  []Skill
	byName  map[string]Skill
	bySlash map[string]Skill
)

func init() {
	byName = make(map[string]Skill, len(bundled))
	bySlash = make(map[string]Skill)
	for _, raw := range bundled {
		s, err := validate(raw)
		if err != nil {
			panic("skill: " + err.Error())
		}
		if _, exists := byName[s.Name]; exists {
			panic(fmt.Sprintf("skill: duplicate name %q", s.Name))
		}
		if s.Slash != "" {
			if _, exists := bySlash[s.Slash]; exists {
				panic(fmt.Sprintf("skill: duplicate slash %q", s.Slash))
			}
			bySlash[s.Slash] = s
		}
		byName[s.Name] = s
		loaded = append(loaded, s)
	}
	sort.Slice(loaded, func(i, j int) bool { return loaded[i].Name < loaded[j].Name })
}

func validate(s Skill) (Skill, error) {
	s.Name = strings.TrimSpace(s.Name)
	s.Description = strings.TrimSpace(s.Description)
	s.Slash = strings.TrimSpace(s.Slash)
	s.Content = strings.TrimSpace(s.Content)
	if s.Name == "" {
		return Skill{}, fmt.Errorf("missing skill name")
	}
	if s.Description == "" {
		return Skill{}, fmt.Errorf("skill %q: missing description", s.Name)
	}
	if s.Content == "" {
		return Skill{}, fmt.Errorf("skill %q: empty body", s.Name)
	}
	if s.Slash != "" {
		if !strings.HasPrefix(s.Slash, "/") || s.Slash == "/" || strings.ContainsAny(s.Slash, " \t\n") {
			return Skill{}, fmt.Errorf("skill %q: invalid slash %q", s.Name, s.Slash)
		}
	}
	return s, nil
}

// All returns bundled skills sorted by name.
func All() []Skill {
	out := make([]Skill, len(loaded))
	copy(out, loaded)
	return out
}

// Get looks up a bundled skill by name.
func Get(name string) (Skill, bool) {
	s, ok := byName[strings.TrimSpace(name)]
	return s, ok
}

// BySlash looks up a skill registered for an exact slash token (e.g. "/review").
func BySlash(token string) (Skill, bool) {
	s, ok := bySlash[strings.TrimSpace(token)]
	return s, ok
}

// MatchSlash reports whether text begins with a registered skill slash as the
// first whitespace-delimited token. Optional trailing args stay on the user
// message; only the token selects the playbook.
func MatchSlash(text string) (Skill, bool) {
	return BySlash(firstToken(text))
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// Catalog returns the system-prompt block listing bundled skills
// (name + description only). Empty when none are bundled.
func Catalog() string {
	if len(loaded) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Skills are bundled playbooks. Call the skill tool when a task matches a description.\n")
	b.WriteString("<available_skills>\n")
	for _, s := range loaded {
		b.WriteString("  <skill>\n")
		b.WriteString("    <name>")
		b.WriteString(s.Name)
		b.WriteString("</name>\n")
		b.WriteString("    <description>")
		b.WriteString(s.Description)
		b.WriteString("</description>\n")
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

// FormatContent wraps the skill body for tool results and slash injection.
func FormatContent(s Skill) string {
	var b strings.Builder
	b.WriteString("<skill_content name=\"")
	b.WriteString(s.Name)
	b.WriteString("\">\n")
	b.WriteString("# Skill: ")
	b.WriteString(s.Name)
	b.WriteString("\n\n")
	b.WriteString(s.Content)
	b.WriteString("\n</skill_content>")
	return b.String()
}

// SlashInjection is the developer-message body for a slash-invoked skill.
func SlashInjection(s Skill) string {
	return FormatContent(s) + "\n\nExecute this skill now. Honor any arguments in the user message after the slash token."
}
