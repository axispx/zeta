package harness

import (
	"encoding/json"
	"testing"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

func reviewSession() *Session {
	return &Session{
		WS: workspace.Context{Abs: "/work", Branch: "main"},
		Rules: permission.NewRules(policy.Policy{Rules: []policy.Rule{
			{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow},
		}}),
	}
}

func bashCall(command, workdir string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": command, "workdir": workdir})
	return b
}

func TestReviewRequest(t *testing.T) {
	s := reviewSession()

	// Only the part no rule covers is put to the reviewer, with the whole
	// command for context.
	req, ok := s.ReviewRequest(tools.Bash, bashCall("go test ./... && make build 2>&1 | tail -5", "sub"))
	if !ok {
		t.Fatal("a splittable command with a pending part should be reviewed")
	}
	if req.Command != "go test ./... && make build 2>&1 | tail -5" || len(req.Pending) != 1 || req.Pending[0] != "make build" ||
		req.Workdir != "sub" || req.Branch != "main" {
		t.Fatalf("request = %+v", req)
	}

	for name, call := range map[string]struct {
		tool string
		args json.RawMessage
	}{
		"other tool":      {tools.Edit, json.RawMessage(`{"path":"a.go"}`)},
		"nothing pending": {tools.Bash, bashCall("go test ./...", "")},
		"dotenv":          {tools.Bash, bashCall("source .env", "")},
		"outside path":    {tools.Bash, bashCall("make -C ../other build", "")},
		"empty":           {tools.Bash, bashCall("", "")},
		// A command that cannot be split is reviewed whole, but the guards on what
		// it names still apply to its text.
		"dotenv in redirect":      {tools.Bash, bashCall("cat .env > out.txt", "")},
		"dotenv in a script":      {tools.Bash, bashCall("python3 - <<'PY'\nprint(open('.env').read())\nPY", "")},
		"outside path in script":  {tools.Bash, bashCall("python3 - <<'PY'\nprint(open('/etc/passwd').read())\nPY", "")},
		"outside path in $(...)":  {tools.Bash, bashCall("make $(cat ~/.ssh/config)", "")},
		"climbing redirect":       {tools.Bash, bashCall("make build > ../out.log", "")},
		"home var in substituion": {tools.Bash, bashCall("make $(cat $HOME/targets)", "")},
	} {
		if _, ok := s.ReviewRequest(call.tool, call.args); ok {
			t.Errorf("%s: must go straight to the prompt", name)
		}
	}
}

// A command nothing can split is the reviewer's to judge whole; it is not sent
// straight to the prompt just because it has a redirect, a here-doc or a
// substitution.
func TestReviewRequestUnsplittable(t *testing.T) {
	defer policy.SetScratchRoots("/tmp")()
	s := reviewSession()
	for name, command := range map[string]string{
		"file redirect": "make build > out.log",
		"substitution":  "make $(cat targets)",
		"here-doc":      "python3 - <<'PY'\nfrom pathlib import Path\nPath('a.txt').write_text('x')\nPY",
		"scratch path":  "go test ./... > /tmp/test.log 2>&1",
		"workspace abs": "git -C /work/sub log > /work/sub/log.txt",
		"home variable": "cd \"$PWD\" && git log --oneline -8",
		"division":      "python3 -c \"print(10 / 2)\" > out.txt",
	} {
		req, ok := s.ReviewRequest(tools.Bash, bashCall(command, ""))
		if !ok {
			t.Errorf("%s: should be reviewed", name)
			continue
		}
		if len(req.Pending) != 1 || req.Pending[0] != command {
			t.Errorf("%s: pending = %q, want the whole command", name, req.Pending)
		}
	}
}

func TestReviewerFollowsConfig(t *testing.T) {
	s := reviewSession()
	// No client: a nil *ai.Client must not become a live backend.
	if s.Reviewer() != nil {
		t.Fatal("no backend available")
	}
	s.Cfg.Review = config.ReviewConfig{Backend: config.ReviewBackendJev, JevAPIKey: "k"}
	if s.Reviewer() == nil {
		t.Fatal("jev chosen with a key")
	}
	s.Cfg.Review = config.ReviewConfig{Disabled: true, Backend: config.ReviewBackendJev, JevAPIKey: "k"}
	if s.Reviewer() != nil {
		t.Fatal("disabled review must not build a reviewer")
	}
}
