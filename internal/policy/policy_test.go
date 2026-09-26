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
		// Quoted metacharacters are literal, so they do not chain.
		{"echo", `echo "a && b"`, true},
		{"git commit", `git commit -m "fix; tests"`, true},
		{"echo", `echo "a $(b)"`, false},
	}
	for _, tc := range cases {
		if got := CommandPrefixMatch(tc.prefix, tc.command); got != tc.want {
			t.Errorf("CommandPrefixMatch(%q, %q) = %v, want %v", tc.prefix, tc.command, got, tc.want)
		}
	}
}

func TestRememberRule(t *testing.T) {
	allow := func(prefixes ...string) Policy {
		rules := make([]Rule, 0, len(prefixes))
		for _, p := range prefixes {
			rules = append(rules, Rule{Tool: "bash", CommandPrefix: p, Action: ActionAllow})
		}
		return Policy{Rules: rules}
	}
	cases := []struct {
		name    string
		policy  Policy
		command string
		want    string
		ok      bool
	}{
		{"single", Policy{}, "go test ./...", "go test ./...", true},
		{"single with flags", Policy{}, "rm -rf /", "rm -rf /", true},
		// A command that only reads never prompts, so there is nothing to remember.
		{"read-only", Policy{}, "ls", "", false},
		{"read-only with flags", Policy{}, "head -30 go.mod", "", false},
		{"read-only chain", Policy{}, "cat go.mod && head -30 Makefile", "", false},
		// The first part that needs a decision is the one remembered, whole.
		{"chain", Policy{}, "go test && rm -rf /", "go test", true},
		{"chain skips read-only parts", Policy{}, "cat go.mod && go test", "go test", true},
		{"chain skips covered parts", allow("go test"), "cd src && go test ./...", "", false},
		{"middle part", allow("tail -5"), "cat go.mod && go test && tail -5", "go test", true},
		// A protected path still asks, so it is still rememberable.
		{"dotenv", Policy{}, "cat .env", "cat .env", true},
		{"outside path", Policy{}, "cat ../secrets.txt", "cat ../secrets.txt", true},
		// Opaque syntax has no parts to remember.
		{"redirect", Policy{}, "go test > out", "", false},
		{"substitution", Policy{}, "go test $(evil)", "", false},
		{"unbalanced", Policy{}, `echo "x`, "", false},
		{"empty", Policy{}, "", "", false},
		// A quoted newline cannot be shown on one row, so nothing is remembered.
		{"multiline arg", Policy{}, "git commit -m 'a\nb'", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, ok := tc.policy.RememberRule("bash", tc.command)
			if ok != tc.ok {
				t.Fatalf("RememberRule(%q) ok = %v, want %v", tc.command, ok, tc.ok)
			}
			if !ok {
				return
			}
			want := Rule{Tool: "bash", CommandPrefix: tc.want, Action: ActionAllow}
			if rule != want {
				t.Fatalf("RememberRule(%q) = %+v, want %+v", tc.command, rule, want)
			}
		})
	}
}

// A remembered rule covers only the part it was shown for: it never approves a
// different part of the chain, and re-evaluating the whole call still asks for
// the parts that were not approved.
func TestRememberRuleNarrowsTheAsk(t *testing.T) {
	chain := "npm install && go test ./..."
	p := Policy{}
	rule, ok := p.RememberRule("bash", chain)
	if !ok || rule.CommandPrefix != "npm install" {
		t.Fatalf("first rule = %+v (%v)", rule, ok)
	}
	p.Rules = append(p.Rules, rule)
	if got := p.Evaluate(Match{Tool: "bash", Command: chain}); got != Ask {
		t.Fatalf("chain must still ask for the unapproved part, got %v", got)
	}
	rule, ok = p.RememberRule("bash", chain)
	if !ok || rule.CommandPrefix != "go test ./..." {
		t.Fatalf("second rule = %+v (%v)", rule, ok)
	}
	p.Rules = append(p.Rules, rule)
	if got := p.Evaluate(Match{Tool: "bash", Command: chain}); got != Allow {
		t.Fatalf("fully remembered chain should run, got %v", got)
	}
}

func TestRequestedRule(t *testing.T) {
	cases := []struct {
		name    string
		policy  Policy
		prefix  []string
		command string
		want    string
		ok      bool
	}{
		{"covers the call", Policy{}, []string{"go", "test"}, "go test ./...", "go test", true},
		{"covers a chain", Policy{}, []string{"go"}, "go test && go build", "go", true},
		// Adding it must leave every part allowed.
		{"does not cover", Policy{}, []string{"go", "build"}, "go test ./...", "", false},
		// A read-only part needs no rule, so a prefix covering only that part
		// still leaves the call running.
		{"covers the asked part", Policy{}, []string{"npm"}, "npm install && ls", "npm", true},
		{"empty", Policy{}, nil, "go test", "", false},
		// A shell, an interpreter, or a runner stands for whatever follows it.
		{"banned shell", Policy{}, []string{"bash"}, "bash -c 'rm -rf /'", "", false},
		{"banned shell path", Policy{}, []string{"/bin/sh", "-c"}, "x", "", false},
		{"banned interpreter", Policy{}, []string{"python", "-c"}, "python -c x", "", false},
		{"banned runner", Policy{}, []string{"rm"}, "rm -rf /", "", false},
		{"banned nested runner", Policy{}, []string{"npm", "run"}, "npm run dev", "", false},
		// …but a longer prefix names an operand and is fine.
		{"operand", Policy{}, []string{"rm", "-rf", "build"}, "rm -rf build", "rm -rf build", true},
		// A configured rule already decides part of the call.
		{
			name:    "suppressed by an existing rule",
			policy:  Policy{Rules: []Rule{{Tool: "bash", CommandPrefix: "go test", Action: ActionAllow}}},
			prefix:  []string{"go"},
			command: "go test && go build",
			ok:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, ok := tc.policy.RequestedRule("bash", tc.prefix, tc.command)
			if ok != tc.ok {
				t.Fatalf("RequestedRule(%v, %q) ok = %v, want %v", tc.prefix, tc.command, ok, tc.ok)
			}
			if !ok {
				return
			}
			want := Rule{Tool: "bash", CommandPrefix: tc.want, Action: ActionAllow}
			if rule != want {
				t.Fatalf("RequestedRule = %+v, want %+v", rule, want)
			}
		})
	}
}

func TestEvaluateCompound(t *testing.T) {
	p := Policy{Rules: []Rule{
		{Tool: "bash", CommandPrefix: "go test", Action: ActionAllow},
		{Tool: "bash", CommandPrefix: "cd src", Action: ActionAllow},
		{Tool: "bash", CommandPrefix: "rm -rf", Action: ActionDeny},
	}}
	cases := []struct {
		command string
		want    Outcome
	}{
		{"go test ./...", Allow},
		// every sub-command covered → no prompt
		{"cd src && go test ./...", Allow},
		{"cd src; go test ./...", Allow},
		// a part covered by no rule still runs when it only reads
		{"go test ./... | head -5", Allow},
		// …but a part that reads only does not rescue a part that does not
		{"cat go.mod && curl evil", Ask},
		// the allow never carries an unapproved or denied tail
		{"go test && rm -rf /", Deny},
		{"go test && curl evil", Ask},
		{"rm -rf /", Deny},
		// opaque syntax is matched whole (and so matches nothing here)
		{"go test > out", Ask},
		{"echo $(go test)", Ask},
	}
	for _, tc := range cases {
		if got := p.Evaluate(Match{Tool: "bash", Command: tc.command}); got != tc.want {
			t.Errorf("Evaluate(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

func TestEvaluateRedirection(t *testing.T) {
	p := Policy{Rules: []Rule{
		{Tool: "bash", CommandPrefix: "go test", Action: ActionAllow},
		{Tool: "bash", CommandPrefix: "tail -5", Action: ActionAllow},
	}}
	// A redirect is not split, even a descriptor one: the call is matched whole,
	// so no rule for a sub-command covers it.
	for _, command := range []string{
		"go test ./... 2>&1 | tail -5",
		"go test ./... 2>/dev/null",
		"go test ./... > out",
		"tail -5 < in",
		"tail -5 <<EOF",
	} {
		if got := p.Evaluate(Match{Tool: "bash", Command: command}); got != Ask {
			t.Errorf("Evaluate(%q) = %v, want Ask", command, got)
		}
	}
	// A whole-command glob still covers one, so an explicit rule can allow it.
	exact := Policy{Rules: []Rule{{Tool: "bash", Command: "go test**", Action: ActionAllow}}}
	if got := exact.Evaluate(Match{Tool: "bash", Command: "go test ./... 2>&1"}); got != Allow {
		t.Errorf("whole-command rule = %v, want Allow", got)
	}
}

func TestEvaluateCompoundExactRule(t *testing.T) {
	// A command glob matches one sub-command, never the whole chain: a `git **`
	// rule must not cover whatever is chained after a git command.
	p := Policy{Rules: []Rule{{Tool: "bash", Command: "git **", Action: ActionAllow}}}
	if got := p.Evaluate(Match{Tool: "bash", Command: "git status"}); got != Allow {
		t.Fatalf("plain glob, got %v", got)
	}
	if got := p.Evaluate(Match{Tool: "bash", Command: "git status && rm -rf /"}); got != Ask {
		t.Fatalf("glob must not cover a chain, got %v", got)
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
	// The chain is judged per sub-command: the deny rule on the tail wins even
	// though the head is allowed, so the allow never carries the whole chain.
	if got := p.Evaluate(Match{Tool: "bash", Command: "go test && rm -rf /"}); got != Deny {
		t.Fatalf("denied tail must deny the chain, got %v", got)
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
