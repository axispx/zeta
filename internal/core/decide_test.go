package core

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
)

func bashArgs(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func TestClassify(t *testing.T) {
	var grants permission.Session
	root := t.TempDir()
	empty := permission.NewRules(policy.Policy{})

	if g := Classify(empty, &grants, root, tools.AskUser, nil); g != WaitInteractive {
		t.Fatalf("ask_user: %v", g)
	}
	if g := Classify(empty, &grants, root, tools.Bash, bashArgs("go test")); g != WaitPermission {
		t.Fatalf("bash ungated: %v", g)
	}
	if g := Classify(empty, &grants, root, tools.Edit, json.RawMessage(`{"path":"a.go"}`)); g != WaitPermission {
		t.Fatalf("edit: %v", g)
	}
	if g := Classify(empty, &grants, root, tools.Read, json.RawMessage(`{"path":"a.go"}`)); g != WaitNone {
		t.Fatalf("in-workspace read: %v", g)
	}
	if g := Classify(empty, &grants, root, tools.Read, json.RawMessage(`{"path":"../x.txt"}`)); g != WaitPermission {
		t.Fatalf("outside read: %v", g)
	}
	if g := Classify(empty, &grants, root, tools.Read, json.RawMessage(`{"path":".env"}`)); g != WaitPermission {
		t.Fatalf(".env read: %v", g)
	}
	if g := Classify(empty, &grants, root, tools.Read, json.RawMessage(`{"path":".env.example"}`)); g != WaitNone {
		t.Fatalf(".env.example: %v", g)
	}

	grants.Grant(tools.Bash)
	if g := Classify(empty, &grants, root, tools.Bash, bashArgs("go test")); g != WaitNone {
		t.Fatalf("bash session-granted: %v", g)
	}
	// edit never session-grantable
	grants.Grant(tools.Edit)
	if g := Classify(empty, &grants, root, tools.Edit, json.RawMessage(`{"path":"a.go"}`)); g != WaitPermission {
		t.Fatalf("edit still waits: %v", g)
	}
	outside := json.RawMessage(`{"path":"../x.txt"}`)
	grants.Grant(tools.Read)
	if g := Classify(empty, &grants, root, tools.Read, outside); g != WaitPermission {
		t.Fatalf("read class grant must not skip outside read: %v", g)
	}
	grants.GrantDir(permission.CallFor(root, tools.Read, outside).Dir)
	if g := Classify(empty, &grants, root, tools.Read, outside); g != WaitNone {
		t.Fatalf("directory grant should skip outside read: %v", g)
	}
	envOutside, _ := json.Marshal(map[string]string{"path": filepath.Join(permission.CallFor(root, tools.Read, outside).Dir, ".env.local")})
	if g := Classify(empty, &grants, root, tools.Read, envOutside); g != WaitPermission {
		t.Fatalf("directory grant must not skip .env.*: %v", g)
	}
	// interactive wins even if somehow permission would also apply
	if g := Classify(empty, &grants, root, tools.AskUser, nil); g != WaitInteractive {
		t.Fatalf("interactive priority: %v", g)
	}
}

func TestClassifyPolicy(t *testing.T) {
	var grants permission.Session
	root := t.TempDir()
	args := bashArgs("go test")

	allow := permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Bash, Command: "go test", Action: policy.ActionAllow}}})
	if g := Classify(allow, &grants, root, tools.Bash, args); g != WaitNone {
		t.Fatalf("allow rule should run: %v", g)
	}

	deny := permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Bash, Command: "go test", Action: policy.ActionDeny}}})
	if g := Classify(deny, &grants, root, tools.Bash, args); g != WaitAutoDeny {
		t.Fatalf("deny rule should auto-deny: %v", g)
	}

	// Deny beats a session grant.
	grants.Grant(tools.Bash)
	if g := Classify(deny, &grants, root, tools.Bash, args); g != WaitAutoDeny {
		t.Fatalf("deny must beat grant: %v", g)
	}

	readDeny := permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Read, Action: policy.ActionDeny}}})
	if g := Classify(readDeny, &grants, root, tools.Read, json.RawMessage(`{"path":"../x.txt"}`)); g != WaitAutoDeny {
		t.Fatalf("tool-level read deny: %v", g)
	}
	readAllow := permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Read, Action: policy.ActionAllow}}})
	if g := Classify(readAllow, &grants, root, tools.Read, json.RawMessage(`{"path":"../x.txt"}`)); g != WaitNone {
		t.Fatalf("tool-level read allow: %v", g)
	}
}

// TestGateMatchesClassify guards the deadlock shape: Gate must wait exactly
// when Classify says the harness will handle the tool start.
func TestGateMatchesClassify(t *testing.T) {
	var grants permission.Session
	root := t.TempDir()
	rules := permission.NewRules(policy.Policy{})
	gate := Gate(rules, &grants, root)

	calls := []struct {
		name string
		args json.RawMessage
	}{
		{tools.AskUser, nil},
		{tools.Bash, bashArgs("go test")},
		{tools.Edit, json.RawMessage(`{"path":"a.go"}`)},
		{tools.Read, json.RawMessage(`{"path":"a.go"}`)},
		{tools.Read, json.RawMessage(`{"path":"../x.txt"}`)},
	}
	for _, c := range calls {
		want := Classify(rules, &grants, root, c.name, c.args) != WaitNone
		if got := gate(c.name, c.args); got != want {
			t.Fatalf("%s: gate=%v classify-waits=%v", c.name, got, want)
		}
	}
}
