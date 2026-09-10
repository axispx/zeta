package policy

import "testing"

func TestGlob(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		// exact (no wildcard)
		{"go test", "go test", true},
		{"go test", "go test -v", false},
		{"src/a.go", "src/a.go", true},
		{"src/a.go", "src/b.go", false},
		// '*' does not cross '/'
		{"go test*", "go test", true},
		{"go test*", "go test -v", true},
		{"go test*", "go test ./...", false}, // '/' not crossed
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/sub/a.go", false},
		{"*", "a/b", false},
		{"*", "abc", true},
		// '**' crosses '/'
		{"go test**", "go test ./...", true},
		{"src/**", "src/a/b.go", true},
		{"src/**", "other/a.go", false},
		{"**/*.go", "a/b/c.go", true},
		{"**/*.go", "c.go", false}, // '**/' still needs the '/'
		{"**", "any/thing", true},
		// '?' is one non-'/' char
		{"a?c", "abc", true},
		{"a?c", "a/c", false},
		{"a?c", "ac", false},
		// empty
		{"", "", true},
		{"", "x", false},
	}
	for _, tc := range cases {
		if got := Glob(tc.pattern, tc.name); got != tc.want {
			t.Errorf("Glob(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestEvaluateDenyPrecedence(t *testing.T) {
	p := Policy{Rules: []Rule{
		{Tool: "bash", Command: "go test*", Action: ActionAllow},
		{Tool: "bash", Command: "go test ./...", Action: ActionDeny},
	}}
	if got := p.Evaluate(Match{Tool: "bash", Command: "go test ./..."}); got != Deny {
		t.Fatalf("deny should win, got %v", got)
	}
	// deny listed first still wins over a later allow
	p = Policy{Rules: []Rule{
		{Tool: "bash", Action: ActionDeny},
		{Tool: "bash", Command: "go test*", Action: ActionAllow},
	}}
	if got := p.Evaluate(Match{Tool: "bash", Command: "go test"}); got != Deny {
		t.Fatalf("deny precedence, got %v", got)
	}
}

func TestEvaluateAllowAndAsk(t *testing.T) {
	p := Policy{Rules: []Rule{
		{Tool: "bash", Command: "go test*", Action: ActionAllow},
	}}
	if got := p.Evaluate(Match{Tool: "bash", Command: "go test -v"}); got != Allow {
		t.Fatalf("allow, got %v", got)
	}
	if got := p.Evaluate(Match{Tool: "bash", Command: "rm -rf /"}); got != Ask {
		t.Fatalf("no match should ask, got %v", got)
	}
	if got := (Policy{}).Evaluate(Match{Tool: "bash", Command: "x"}); got != Ask {
		t.Fatalf("empty policy should ask, got %v", got)
	}
}

func TestEvaluateToolLevel(t *testing.T) {
	p := Policy{Rules: []Rule{{Tool: "edit", Action: ActionAllow}}}
	if got := p.Evaluate(Match{Tool: "edit", Path: "a.go"}); got != Allow {
		t.Fatalf("tool-level allow, got %v", got)
	}
	if got := p.Evaluate(Match{Tool: "write", Path: "a.go"}); got != Ask {
		t.Fatalf("other tool, got %v", got)
	}
	// "*" matches every tool
	p = Policy{Rules: []Rule{{Tool: "*", Action: ActionDeny}}}
	if got := p.Evaluate(Match{Tool: "bash", Command: "ls"}); got != Deny {
		t.Fatalf("wildcard tool, got %v", got)
	}
}

func TestEvaluatePathRuleNeedsPath(t *testing.T) {
	p := Policy{Rules: []Rule{{Tool: "edit", Path: "**", Action: ActionAllow}}}
	// Empty Match.Path (out-of-workspace target) never matches a path rule.
	if got := p.Evaluate(Match{Tool: "edit"}); got != Ask {
		t.Fatalf("empty path must not match path rule, got %v", got)
	}
	if got := p.Evaluate(Match{Tool: "edit", Path: "deep/a.go"}); got != Allow {
		t.Fatalf("path rule, got %v", got)
	}
}

func TestEvaluateCommandRuleNeedsCommand(t *testing.T) {
	p := Policy{Rules: []Rule{{Tool: "bash", Command: "*", Action: ActionAllow}}}
	if got := p.Evaluate(Match{Tool: "bash"}); got != Ask {
		t.Fatalf("empty command must not match command rule, got %v", got)
	}
}

func TestCommandPrefixMatch(t *testing.T) {
	cases := []struct {
		prefix  string
		command string
		want    bool
	}{
		{"go test", "go test", true},
		{"go test", "go test -v", true},
		{"go test", "go test ./...", true},
		{"go test", "gotest", false},
		{"go test", "go testify", false},
		{"rm -rf /", "rm -rf /", true},
		// Not simple → never matched by a prefix rule.
		{"go test", "go test && rm -rf /", false},
		{"go test", "go test | sh", false},
		{"go test", "go test; ls", false},
		{"go test", "go test $(evil)", false},
		{"go test", "go test > out", false},
		{"", "go test", false},
	}
	for _, tc := range cases {
		if got := CommandPrefixMatch(tc.prefix, tc.command); got != tc.want {
			t.Errorf("CommandPrefixMatch(%q, %q) = %v, want %v", tc.prefix, tc.command, got, tc.want)
		}
	}
}

func TestDeriveCommandPrefix(t *testing.T) {
	cases := []struct {
		command string
		want    string
		ok      bool
	}{
		{"go test ./...", "go test", true},
		{"go test -v", "go test", true},
		{"git commit -m x", "git commit", true},
		{"npm install left-pad", "npm install", true},
		{"ls", "ls", true},
		// flag immediately after the program: keep the whole command, no broadening
		{"rm -rf /", "rm -rf /", true},
		{"ls -la", "ls -la", true},
		{"python -c evil", "python -c evil", true},
		// not simple
		{"go test && rm -rf /", "", false},
		{"", "", false},
		{"   ", "", false},
	}
	for _, tc := range cases {
		got, ok := DeriveCommandPrefix(tc.command)
		if got != tc.want || ok != tc.ok {
			t.Errorf("DeriveCommandPrefix(%q) = (%q, %v), want (%q, %v)", tc.command, got, ok, tc.want, tc.ok)
		}
	}
}

func TestEvaluateCommandPrefix(t *testing.T) {
	p := Policy{Rules: []Rule{
		{Tool: "bash", CommandPrefix: "go test", Action: ActionAllow},
		{Tool: "bash", CommandPrefix: "rm -rf", Action: ActionDeny},
	}}
	if got := p.Evaluate(Match{Tool: "bash", Command: "go test ./..."}); got != Allow {
		t.Fatalf("prefix allow, got %v", got)
	}
	// Chain guard keeps a chained command from matching the prefix allow.
	if got := p.Evaluate(Match{Tool: "bash", Command: "go test && rm -rf /"}); got != Ask {
		t.Fatalf("chained must not match prefix, got %v", got)
	}
	// Deny prefix wins over an allow prefix match.
	p2 := Policy{Rules: []Rule{
		{Tool: "bash", CommandPrefix: "go test", Action: ActionAllow},
		{Tool: "bash", CommandPrefix: "go test", Action: ActionDeny},
	}}
	if got := p2.Evaluate(Match{Tool: "bash", Command: "go test -v"}); got != Deny {
		t.Fatalf("deny precedence, got %v", got)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate([]Rule{{Tool: "bash", Action: ActionAllow}}); err != nil {
		t.Fatalf("valid rule: %v", err)
	}
	if err := Validate([]Rule{{Tool: "", Action: ActionAllow}}); err == nil {
		t.Fatal("missing tool should fail")
	}
	if err := Validate([]Rule{{Tool: "bash", Action: "maybe"}}); err == nil {
		t.Fatal("bad action should fail")
	}
	if err := Validate([]Rule{{Tool: "bash", Command: "x", Path: "y", Action: ActionAllow}}); err == nil {
		t.Fatal("command+path should fail")
	}
	if err := Validate([]Rule{{Tool: "bash", CommandPrefix: "x", Path: "y", Action: ActionAllow}}); err == nil {
		t.Fatal("command_prefix+path should fail")
	}
	if err := Validate([]Rule{{Tool: "bash", CommandPrefix: "go test", Action: ActionAllow}}); err != nil {
		t.Fatal("command_prefix should be valid")
	}
}
