package harness

import (
	"encoding/json"
	"testing"

	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
)

func choiceKeys(a Approval) string {
	out := ""
	for _, d := range a.Choices {
		switch d {
		case permission.AllowOnce:
			out += "a"
		case permission.AllowAlways:
			out += "p"
		case permission.AllowSession:
			out += "s"
		case permission.Deny:
			out += "d"
		}
	}
	return out
}

func TestApprovalForChoices(t *testing.T) {
	root := t.TempDir()
	plan := policy.Policy{}
	cases := []struct {
		name    string
		tool    string
		args    json.RawMessage
		want    string
		env     bool
		outside bool
	}{
		{"bash remembers", tools.Bash, bashArgs("go test"), "apsd", false, false},
		{"bash no command", tools.Bash, json.RawMessage(`{}`), "asd", false, false},
		{"edit once-only", tools.Edit, json.RawMessage(`{"path":"a.go"}`), "ad", false, false},
		{"outside edit once-only", tools.Edit, json.RawMessage(`{"path":"../x.txt"}`), "ad", false, true},
		{"outside read grants dir", tools.Read, json.RawMessage(`{"path":"../x.txt"}`), "asd", false, true},
		{"env read remembers file", tools.Read, json.RawMessage(`{"path":".env"}`), "apd", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := ApprovalFor(plan, root, tc.tool, tc.args)
			if got := choiceKeys(a); got != tc.want {
				t.Fatalf("choices = %q, want %q", got, tc.want)
			}
			if a.Env != tc.env || a.Call.Outside != tc.outside {
				t.Fatalf("env=%v outside=%v", a.Env, a.Call.Outside)
			}
		})
	}
}

// The prompt writes the rule Approval.Call carries; the two must not drift.
func TestApprovalForPersistRule(t *testing.T) {
	root := t.TempDir()
	plan := policy.Policy{}

	bash := ApprovalFor(plan, root, tools.Bash, bashArgs("go test"))
	if !bash.Call.Persist {
		t.Fatal("a simple command should be rememberable")
	}
	want := policy.Rule{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow}
	if bash.Call.Rule != want {
		t.Fatalf("rule = %+v, want %+v", bash.Call.Rule, want)
	}

	// A chain remembers its first part that still needs a decision, and only
	// that one. A read-only part needs no decision, so it is skipped.
	chain := ApprovalFor(plan, root, tools.Bash, bashArgs("cd src && go test ./..."))
	if !chain.Call.Persist {
		t.Fatal("a plain chain should be rememberable")
	}
	if want := (policy.Rule{Tool: tools.Bash, CommandPrefix: "go test ./...", Action: policy.ActionAllow}); chain.Call.Rule != want {
		t.Fatalf("chain rule = %+v, want %+v", chain.Call.Rule, want)
	}

	// A chain of read-only parts runs on its own, so there is nothing to write.
	if a := ApprovalFor(plan, root, tools.Bash, bashArgs("cd src && ls -la")); a.Call.Persist {
		t.Fatalf("read-only chain must not be rememberable: %+v", a.Call)
	}

	covered := plan
	covered.Rules = append(covered.Rules, chain.Call.Rule)
	if a := ApprovalFor(covered, root, tools.Bash, bashArgs("cd src && go test ./...")); a.Call.Persist {
		t.Fatalf("covered chain must not be rememberable: %+v", a.Call)
	}

	// A model-proposed prefix replaces the derived part rule when it is sound.
	proposed := ApprovalFor(plan, root, tools.Bash, bashArgsWithPrefix("go test ./...", "go", "test"))
	if want := (policy.Rule{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow}); proposed.Call.Rule != want {
		t.Fatalf("proposed rule = %+v, want %+v", proposed.Call.Rule, want)
	}
	// A banned one is refused and the derived rule stands.
	banned := ApprovalFor(plan, root, tools.Bash, bashArgsWithPrefix("bash -c 'x'", "bash"))
	if want := (policy.Rule{Tool: tools.Bash, CommandPrefix: "bash -c 'x'", Action: policy.ActionAllow}); banned.Call.Rule != want {
		t.Fatalf("banned prefix must fall back: %+v", banned.Call)
	}

	// A file-less redirect is dropped: nothing here needs approval, so there is no
	// rule to write for it.
	redirected := ApprovalFor(plan, root, tools.Bash, bashArgs("cat go.mod 2>/dev/null | head -20"))
	if redirected.Call.Persist {
		t.Fatalf("a redirected call must not be rememberable: %+v", redirected.Call)
	}

	opaque := ApprovalFor(plan, root, tools.Bash, bashArgs("go test ./... > out.txt"))
	if opaque.Call.Persist {
		t.Fatalf("a file redirect must not be rememberable: %+v", opaque.Call)
	}

	env := ApprovalFor(plan, root, tools.Read, json.RawMessage(`{"path":".env"}`))
	if !env.Call.Persist {
		t.Fatal("an in-workspace dotenv read should be rememberable")
	}
	if want := (policy.Rule{Tool: tools.Read, Path: ".env", Action: policy.ActionAllow}); env.Call.Rule != want {
		t.Fatalf("rule = %+v, want %+v", env.Call.Rule, want)
	}

	// An outside read is not rememberable, but carries the granted directory.
	outside := ApprovalFor(plan, root, tools.Read, json.RawMessage(`{"path":"../x.txt"}`))
	if outside.Call.Persist {
		t.Fatal("an outside read must not persist a rule")
	}
	if outside.Call.Dir == "" {
		t.Fatal("an outside read should carry a session-grant directory")
	}

	edit := ApprovalFor(plan, root, tools.Edit, json.RawMessage(`{"path":"a.go"}`))
	if !edit.Call.Persist {
		t.Fatal("an in-workspace edit target is rememberable in principle")
	}
	if len(edit.Choices) != 2 {
		t.Fatalf("edit must stay allow-or-deny: %+v", edit.Choices)
	}
}
