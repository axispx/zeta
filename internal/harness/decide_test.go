package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
)

func bashArgs(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

// bashArgsWithPrefix is a bash call whose model proposed remembering prefix.
func bashArgsWithPrefix(cmd string, prefix ...string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"command": cmd, "prefix_rule": prefix})
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
	outsideFile, _ := json.Marshal(map[string]string{"path": writeFixture(t, filepath.Join(t.TempDir(), "x.txt")), "reason": "test"})
	if g := Classify(empty, &grants, root, tools.Read, outsideFile); g != WaitPermission {
		t.Fatalf("outside read: %v", g)
	}
	if g := Classify(empty, &grants, root, tools.Read, json.RawMessage(`{"path":".env"}`)); g != WaitPermission {
		t.Fatalf(".env read: %v", g)
	}
	if g := Classify(empty, &grants, root, tools.Read, json.RawMessage(`{"path":".env.example"}`)); g != WaitNone {
		t.Fatalf(".env.example: %v", g)
	}

	// A session grant is the approved command, not the shell tool.
	grants.GrantCmd("go test")
	if g := Classify(empty, &grants, root, tools.Bash, bashArgs("go test")); g != WaitNone {
		t.Fatalf("granted command should run: %v", g)
	}
	if g := Classify(empty, &grants, root, tools.Bash, bashArgs("rm -rf /")); g != WaitPermission {
		t.Fatalf("a different command must still ask: %v", g)
	}
	// edit never session-grantable
	if g := Classify(empty, &grants, root, tools.Edit, json.RawMessage(`{"path":"a.go"}`)); g != WaitPermission {
		t.Fatalf("edit still waits: %v", g)
	}
	outsidePath := writeFixture(t, filepath.Join(t.TempDir(), "x.txt"))
	outside, _ := json.Marshal(map[string]string{"path": outsidePath, "reason": "test"})
	if g := Classify(empty, &grants, root, tools.Read, outside); g != WaitPermission {
		t.Fatalf("a command grant must not skip an outside read: %v", g)
	}
	grants.GrantDir(permission.CallFor(policy.Policy{}, root, tools.Read, outside).Dir)
	if g := Classify(empty, &grants, root, tools.Read, outside); g != WaitNone {
		t.Fatalf("directory grant should skip outside read: %v", g)
	}
	envOutside, _ := json.Marshal(map[string]string{"path": filepath.Join(permission.CallFor(policy.Policy{}, root, tools.Read, outside).Dir, ".env.local"), "reason": "test"})
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
	grants.GrantCmd("go test")
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
		{tools.Read, json.RawMessage(`{"path":"../x.txt","reason":"test"}`)},
	}
	for _, c := range calls {
		want := Classify(rules, &grants, root, c.name, c.args) != WaitNone
		if got := gate(c.name, c.args); got != want {
			t.Fatalf("%s: gate=%v classify-waits=%v", c.name, got, want)
		}
	}
}

func TestDecidePermissionSessionGrantAndDeny(t *testing.T) {
	root := t.TempDir()
	var grants permission.Session
	s := &Session{Grants: &grants, Rules: permission.NewRules(policy.Policy{})}

	bash := permission.CallFor(policy.Policy{}, root, tools.Bash, bashArgs("go test"))
	reply, err := s.DecidePermission(permission.AllowSession, bash, "")
	if err != nil || reply.Kind != ReplyRun {
		t.Fatalf("grant: reply=%+v err=%v", reply, err)
	}
	if !grants.CmdGranted("go test") {
		t.Fatal("the approved command should be granted for the session")
	}
	// The session row must not turn one approval into a shell-wide grant.
	if grants.CmdGranted("rm -rf /") {
		t.Fatal("unrelated commands must not be granted")
	}

	// An outside read grants the directory, not the read class.
	outside := permission.CallFor(policy.Policy{}, root, tools.Read, json.RawMessage(`{"path":"../x.txt"}`))
	if _, err := s.DecidePermission(permission.AllowSession, outside, ""); err != nil {
		t.Fatal(err)
	}
	if !grants.DirGranted(outside) {
		t.Fatal("outside directory should be granted")
	}

	if r, err := s.DecidePermission(permission.Deny, bash, ""); err != nil || r.Kind != ReplyDeny {
		t.Fatalf("deny: reply=%+v err=%v", r, err)
	}
}

func TestDecidePermissionDenyReason(t *testing.T) {
	root := t.TempDir()
	s := &Session{Grants: &permission.Session{}, Rules: permission.NewRules(policy.Policy{})}
	bash := permission.CallFor(policy.Policy{}, root, tools.Bash, bashArgs("go test"))

	// A typed reason reaches the model instead of the generic denial.
	r, err := s.DecidePermission(permission.Deny, bash, "use make test instead")
	if err != nil || r.Kind != ReplyDeny {
		t.Fatalf("reply=%+v err=%v", r, err)
	}
	if r.Reason != "use make test instead" {
		t.Fatalf("reason=%q", r.Reason)
	}

	// Empty / whitespace reason falls back to the generic denial.
	if r, _ := s.DecidePermission(permission.Deny, bash, "  "); r.Reason != "the user denied this call" {
		t.Fatalf("blank reason should stay generic: %q", r.Reason)
	}
}

func TestDecidePermissionAllowAlwaysPersists(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	root := t.TempDir()
	rules := permission.NewRules(policy.Policy{})
	s := &Session{Grants: &permission.Session{}, Rules: rules}

	reply, err := s.DecidePermission(permission.AllowAlways, permission.CallFor(policy.Policy{}, root, tools.Bash, bashArgs("go test")), "")
	if err != nil || reply.Kind != ReplyRun {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	want := policy.Rule{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow}
	if got := rules.Policy().Rules; len(got) != 1 || got[0] != want {
		t.Fatalf("live rules=%+v", got)
	}
	loaded, err := policy.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Rules) != 1 || loaded.Rules[0] != want {
		t.Fatalf("persisted=%+v", loaded)
	}

	// A chain persists one rule: the first part that still needs a decision.
	// `cd src` only reads and `go test ./...` is already covered, so the new
	// rule is the part in between.
	reply, err = s.DecidePermission(permission.AllowAlways, permission.CallFor(rules.Policy(), root, tools.Bash, bashArgs("cd src && npm install && go test ./...")), "")
	if err != nil || reply.Kind != ReplyRun {
		t.Fatalf("chain reply=%+v err=%v", reply, err)
	}
	wantChain := []policy.Rule{
		want,
		{Tool: tools.Bash, CommandPrefix: "npm install", Action: policy.ActionAllow},
	}
	if got := rules.Policy().Rules; !slices.Equal(got, wantChain) {
		t.Fatalf("chain rules=%+v", got)
	}
	loaded, err = policy.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(loaded.Rules, wantChain) {
		t.Fatalf("persisted chain=%+v", loaded)
	}

	// A chain whose parts all run already has nothing to write.
	if reply, err = s.DecidePermission(permission.AllowAlways, permission.CallFor(rules.Policy(), root, tools.Bash, bashArgs("cd src && go test ./...")), ""); err != nil || reply.Kind != ReplyRun {
		t.Fatalf("covered chain reply=%+v err=%v", reply, err)
	}
	if got := rules.Policy().Rules; !slices.Equal(got, wantChain) {
		t.Fatalf("covered chain must add nothing: %+v", got)
	}
}

func TestDecidePermissionPersistFailureStillAllows(t *testing.T) {
	// A regular file where ZETA_HOME should be makes policy.Save fail.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZETA_HOME", filepath.Join(file, "zeta"))
	root := t.TempDir()
	rules := permission.NewRules(policy.Policy{})
	s := &Session{Grants: &permission.Session{}, Rules: rules}

	reply, err := s.DecidePermission(permission.AllowAlways, permission.CallFor(policy.Policy{}, root, tools.Bash, bashArgs("go test")), "")
	if err == nil {
		t.Fatal("expected a persist error")
	}
	if reply.Kind != ReplyRun {
		t.Fatalf("decision must survive a failed persist: %+v", reply)
	}
	if len(rules.Policy().Rules) != 0 {
		t.Fatalf("failed persist must not install a rule: %+v", rules.Policy())
	}
}

// writeFixture creates path so an outside read has a real target to approve.
func writeFixture(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOutsideReadNeedsReason(t *testing.T) {
	root := t.TempDir()
	var grants permission.Session
	empty := permission.NewRules(policy.Policy{})
	noReason := writeFixture(t, filepath.Join(t.TempDir(), "x.txt"))
	bare, _ := json.Marshal(map[string]string{"path": noReason})
	if g := Classify(empty, &grants, root, tools.Read, bare); g != WaitNeedsReason {
		t.Fatalf("outside read without reason: %v", g)
	}
	if r, ok := AutoReply(WaitNeedsReason); !ok || r.Kind != ReplyDeny || r.Reason != NeedsReasonReason {
		t.Fatalf("auto reply = %+v, %v", r, ok)
	}
	withReason, _ := json.Marshal(map[string]string{"path": noReason, "reason": "check the spec"})
	if g := Classify(empty, &grants, root, tools.Read, withReason); g != WaitPermission {
		t.Fatalf("outside read with reason should prompt: %v", g)
	}
	missing, _ := json.Marshal(map[string]string{"path": filepath.Join(t.TempDir(), "nope")})
	if g := Classify(empty, &grants, root, tools.Read, missing); g != WaitNone {
		t.Fatalf("missing outside read has nothing to approve: %v", g)
	}
	edit, _ := json.Marshal(map[string]string{"path": noReason})
	if g := Classify(empty, &grants, root, tools.Edit, edit); g != WaitPermission {
		t.Fatalf("outside edit does not need a reason: %v", g)
	}
}
