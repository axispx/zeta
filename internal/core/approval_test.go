package core

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
			a := ApprovalFor(root, tc.tool, tc.args)
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
	bash := ApprovalFor(root, tools.Bash, bashArgs("go test"))
	if !bash.Call.Persist {
		t.Fatal("a simple command should be rememberable")
	}
	if want := (policy.Rule{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow}); bash.Call.Rule != want {
		t.Fatalf("rule = %+v, want %+v", bash.Call.Rule, want)
	}

	env := ApprovalFor(root, tools.Read, json.RawMessage(`{"path":".env"}`))
	if !env.Call.Persist {
		t.Fatal("an in-workspace dotenv read should be rememberable")
	}
	if want := (policy.Rule{Tool: tools.Read, Path: ".env", Action: policy.ActionAllow}); env.Call.Rule != want {
		t.Fatalf("rule = %+v, want %+v", env.Call.Rule, want)
	}

	// An outside read is not rememberable, but carries the granted directory.
	outside := ApprovalFor(root, tools.Read, json.RawMessage(`{"path":"../x.txt"}`))
	if outside.Call.Persist {
		t.Fatal("an outside read must not persist a rule")
	}
	if outside.Call.Dir == "" {
		t.Fatal("an outside read should carry a session-grant directory")
	}

	edit := ApprovalFor(root, tools.Edit, json.RawMessage(`{"path":"a.go"}`))
	if !edit.Call.Persist {
		t.Fatal("an in-workspace edit target is rememberable in principle")
	}
	if len(edit.Choices) != 2 {
		t.Fatalf("edit must stay allow-or-deny: %+v", edit.Choices)
	}
}
