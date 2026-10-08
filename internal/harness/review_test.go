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
		"file redirect":   {tools.Bash, bashCall("make build > out.log", "")},
		"substitution":    {tools.Bash, bashCall("make $(cat targets)", "")},
		"dotenv":          {tools.Bash, bashCall("source .env", "")},
		"outside path":    {tools.Bash, bashCall("make -C ../other build", "")},
		"empty":           {tools.Bash, bashCall("", "")},
	} {
		if _, ok := s.ReviewRequest(call.tool, call.args); ok {
			t.Errorf("%s: must go straight to the prompt", name)
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
