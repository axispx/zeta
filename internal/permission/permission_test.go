package permission

import (
	"encoding/json"
	"testing"

	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
)

func TestClassifyPrecedence(t *testing.T) {
	root := t.TempDir()
	bashArgs := json.RawMessage(`{"command":"go test"}`)

	// Policy deny beats a session grant.
	var grants Session
	grants.Grant(tools.Bash)
	deny := policy.Policy{Rules: []policy.Rule{{Tool: tools.Bash, Action: policy.ActionDeny}}}
	if got := Classify(NewRules(deny), &grants, root, tools.Bash, bashArgs); got != policy.Deny {
		t.Fatalf("deny must beat grant, got %v", got)
	}

	// Session grant runs when no deny matches.
	if got := Classify(NewRules(policy.Policy{}), &grants, root, tools.Bash, bashArgs); got != policy.Allow {
		t.Fatalf("grant, got %v", got)
	}

	// Policy allow runs without a grant.
	var none Session
	allow := policy.Policy{Rules: []policy.Rule{{Tool: tools.Bash, Command: "go test", Action: policy.ActionAllow}}}
	if got := Classify(NewRules(allow), &none, root, tools.Bash, bashArgs); got != policy.Allow {
		t.Fatalf("allow, got %v", got)
	}

	// Unmatched side-effect tool asks.
	if got := Classify(NewRules(policy.Policy{}), &none, root, tools.Bash, bashArgs); got != policy.Ask {
		t.Fatalf("ask, got %v", got)
	}
}

func TestClassifyNilRules(t *testing.T) {
	var none Session
	if got := Classify(nil, &none, t.TempDir(), tools.Bash, json.RawMessage(`{"command":"ls"}`)); got != policy.Ask {
		t.Fatalf("nil rules should ask, got %v", got)
	}
}

func TestClassifyNonSideEffectRuns(t *testing.T) {
	var none Session
	if got := Classify(NewRules(policy.Policy{}), &none, t.TempDir(), tools.Read, json.RawMessage(`{"path":"a.go"}`)); got != policy.Allow {
		t.Fatalf("read should run, got %v", got)
	}
}

func TestClassifyNilSession(t *testing.T) {
	if got := Classify(NewRules(policy.Policy{}), nil, t.TempDir(), tools.Bash, json.RawMessage(`{"command":"ls"}`)); got != policy.Ask {
		t.Fatalf("nil session, got %v", got)
	}
}

func TestClassifyOutsideWorkspaceNeverMatchesPathRule(t *testing.T) {
	root := t.TempDir()
	pol := NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Edit, Path: "**", Action: policy.ActionAllow}}})
	var none Session
	args := json.RawMessage(`{"path":"../x.txt"}`)
	if got := Classify(pol, &none, root, tools.Edit, args); got != policy.Ask {
		t.Fatalf("outside edit should ask, got %v", got)
	}
	// Same relative path inside the workspace matches.
	inside := json.RawMessage(`{"path":"x.txt"}`)
	if got := Classify(pol, &none, root, tools.Edit, inside); got != policy.Allow {
		t.Fatalf("in-tree edit should run, got %v", got)
	}
}

func TestRulesReplaceInPlace(t *testing.T) {
	rules := NewRules(policy.Policy{})
	var none Session
	args := json.RawMessage(`{"command":"go test -v"}`)
	if got := Classify(rules, &none, t.TempDir(), tools.Bash, args); got != policy.Ask {
		t.Fatalf("no rules should ask, got %v", got)
	}
	rules.Replace(policy.Policy{Rules: []policy.Rule{{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow}}})
	if got := Classify(rules, &none, t.TempDir(), tools.Bash, args); got != policy.Allow {
		t.Fatalf("replaced rules must be visible, got %v", got)
	}
}

func TestCallFor(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name        string
		tool        string
		args        string
		wantMatch   policy.Match
		wantRule    policy.Rule
		wantPersist bool
		wantOutside bool
	}{
		{
			name: "bash-prefix", tool: tools.Bash, args: `{"command":"  go test ./...  "}`,
			wantMatch:   policy.Match{Tool: tools.Bash, Command: "go test ./..."},
			wantRule:    policy.Rule{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow},
			wantPersist: true,
		},
		{
			name: "bash-chained", tool: tools.Bash, args: `{"command":"go test && rm -rf /"}`,
			wantMatch: policy.Match{Tool: tools.Bash, Command: "go test && rm -rf /"},
		},
		{
			name: "edit-inside", tool: tools.Edit, args: `{"path":"src/a.go"}`,
			wantMatch:   policy.Match{Tool: tools.Edit, Path: "src/a.go"},
			wantRule:    policy.Rule{Tool: tools.Edit, Path: "src/a.go", Action: policy.ActionAllow},
			wantPersist: true,
		},
		{
			name: "edit-outside", tool: tools.Edit, args: `{"path":"../a.go"}`,
			wantMatch:   policy.Match{Tool: tools.Edit},
			wantOutside: true,
		},
		{
			name: "write-subdir", tool: tools.Write, args: `{"path":"sub/a.go"}`,
			wantMatch:   policy.Match{Tool: tools.Write, Path: "sub/a.go"},
			wantRule:    policy.Rule{Tool: tools.Write, Path: "sub/a.go", Action: policy.ActionAllow},
			wantPersist: true,
		},
		{
			name: "read-no-rule", tool: tools.Read, args: `{"path":"a.go"}`,
			wantMatch: policy.Match{Tool: tools.Read},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CallFor(root, tc.tool, json.RawMessage(tc.args))
			if got.Match != tc.wantMatch {
				t.Errorf("Match = %+v, want %+v", got.Match, tc.wantMatch)
			}
			if got.Rule != tc.wantRule || got.Persist != tc.wantPersist {
				t.Errorf("Rule/Persist = (%+v, %v), want (%+v, %v)", got.Rule, got.Persist, tc.wantRule, tc.wantPersist)
			}
			if got.Outside != tc.wantOutside {
				t.Errorf("Outside = %v, want %v", got.Outside, tc.wantOutside)
			}
		})
	}
}

func TestSideEffect(t *testing.T) {
	for _, name := range []string{tools.Bash, tools.Edit, tools.Write} {
		if !SideEffect(name) {
			t.Errorf("%s should be side-effect", name)
		}
	}
	for _, name := range []string{tools.Read, tools.Grep, tools.Glob, tools.WebSearch, tools.WebFetch, tools.Skill, ""} {
		if SideEffect(name) {
			t.Errorf("%s should not be side-effect", name)
		}
	}
}

func TestClassOf(t *testing.T) {
	if c, ok := ClassOf(tools.Bash); !ok || c != ClassBash {
		t.Fatalf("bash: %v %v", c, ok)
	}
	if c, ok := ClassOf(tools.Write); !ok || c != ClassEdit {
		t.Fatalf("write: %v %v", c, ok)
	}
	if _, ok := ClassOf(tools.Read); ok {
		t.Fatal("read has no class")
	}
}

func TestSessionGrantable(t *testing.T) {
	if !SessionGrantable(tools.Bash) {
		t.Fatal("bash should be session-grantable")
	}
	for _, name := range []string{tools.Edit, tools.Write, tools.Read, ""} {
		if SessionGrantable(name) {
			t.Fatalf("%s must not be session-grantable", name)
		}
	}
}

func TestSessionGrant(t *testing.T) {
	var s Session
	if s.Granted(tools.Bash) {
		t.Fatal("empty session")
	}
	s.Grant(tools.Bash)
	if !s.Granted(tools.Bash) {
		t.Fatal("bash grant")
	}
	s.Grant(tools.Edit)
	if s.Granted(tools.Edit) || s.Granted(tools.Write) {
		t.Fatal("edit/write must never receive a session grant")
	}
	if s.Granted(tools.Read) {
		t.Fatal("read has no class")
	}
}

func TestNilSession(t *testing.T) {
	var s *Session
	if s.Granted(tools.Bash) {
		t.Fatal("nil")
	}
	s.Grant(tools.Bash) // must not panic
	var none Session
	if got := Classify(nil, &none, t.TempDir(), tools.Bash, json.RawMessage(`{"command":"ls"}`)); got != policy.Ask {
		t.Fatalf("bash needs decision, got %v", got)
	}
	if got := Classify(nil, &none, t.TempDir(), tools.Edit, json.RawMessage(`{"path":"a.go"}`)); got != policy.Ask {
		t.Fatalf("edit needs decision, got %v", got)
	}
	if got := Classify(nil, &none, t.TempDir(), tools.Read, json.RawMessage(`{"path":"a.go"}`)); got != policy.Allow {
		t.Fatalf("read never needs decision, got %v", got)
	}
}

func TestGrantNeverSkipsEditPrompt(t *testing.T) {
	var s Session
	s.Grant(tools.Edit) // no-op: edit is not session-grantable
	if got := Classify(nil, &s, t.TempDir(), tools.Edit, json.RawMessage(`{"path":"a.go"}`)); got != policy.Ask {
		t.Fatalf("edit must still ask after Grant, got %v", got)
	}
	if got := Classify(nil, &s, t.TempDir(), tools.Write, json.RawMessage(`{"path":"a.go"}`)); got != policy.Ask {
		t.Fatalf("write must still ask after Grant, got %v", got)
	}
}
