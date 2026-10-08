package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/classifier"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/harness"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

func reviewModel(t *testing.T) (*Model, chan harness.Reply) {
	t.Helper()
	isolateZetaHome(t)
	t.Setenv("TYPESAFE_API_KEY", "")
	m := testModel()
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.session.Grants = &permission.Session{}
	m.session.Rules = permission.NewRules(policy.Policy{})
	replies := make(chan harness.Reply, 1)
	m.turn.current = &turnSession{id: 1, activeTool: -1, ch: make(chan harness.Event), reply: replies, cancel: func() {}}
	return m, replies
}

func TestReviewOffOpensPrompt(t *testing.T) {
	m, _ := reviewModel(t)
	_ = m.handleTurnToolStart(turnToolStartMsg{id: 1, name: tools.Bash, label: "bash make build", args: bashArgs("make build")})
	if m.panel.perm == nil {
		t.Fatal("with review off the prompt opens at once")
	}
}

func TestReviewHoldsPromptUntilVerdict(t *testing.T) {
	m, replies := reviewModel(t)
	m.session.Cfg.Review = config.ReviewConfig{Enabled: true, JevAPIKey: "k"}

	cmd := m.handleTurnToolStart(turnToolStartMsg{id: 1, name: tools.Bash, label: "bash make build", args: bashArgs("make build")})
	if cmd == nil || m.panel.perm != nil {
		t.Fatal("a reviewable command waits for the verdict with no prompt open")
	}
	select {
	case r := <-replies:
		t.Fatalf("nothing answers the loop before the verdict: %+v", r)
	default:
	}

	// A command the reviewer must not see still prompts at once.
	m.panel.clear()
	_ = m.handleTurnToolStart(turnToolStartMsg{id: 1, name: tools.Bash, label: "bash cat .env", args: bashArgs("cat .env")})
	if m.panel.perm == nil {
		t.Fatal("a dotenv read must prompt without a review")
	}
}

func TestReviewVerdicts(t *testing.T) {
	start := turnToolStartMsg{id: 1, name: tools.Bash, label: "bash make build", args: bashArgs("make build")}

	// Approved: the loop is told to run, and no prompt opens.
	m, replies := reviewModel(t)
	m.handleReviewDone(reviewDoneMsg{id: 1, start: start, verdict: classifier.Verdict{
		Approved: true, Result: classifier.Result{Label: classifier.LocalReversible, Source: "jev"},
	}})
	if r := <-replies; r.Kind != harness.ReplyRun {
		t.Fatalf("approved review should run the call: %+v", r)
	}
	if len(m.transcript.messages) != 0 {
		t.Fatal("an approval leaves no note")
	}
	if m.panel.perm != nil {
		t.Fatal("an approved command must not prompt")
	}

	// Not approved: the usual prompt, and the loop stays waiting for the user.
	m, replies = reviewModel(t)
	rows := len(m.transcript.messages)
	m.handleReviewDone(reviewDoneMsg{id: 1, start: start, verdict: classifier.Verdict{
		Result:  classifier.Result{Label: classifier.ReadOnly, Source: "jev"},
		Concern: classifier.Risky,
	}})
	if m.panel.perm == nil {
		t.Fatal("a refused review falls back to the prompt")
	}
	const want = "Auto review: may be destructive or send data out"
	if m.panel.perm.review != want {
		t.Fatalf("prompt review line = %q", m.panel.perm.review)
	}
	if len(m.transcript.messages) != rows {
		t.Fatal("the verdict belongs in the prompt, not the transcript")
	}
	// The line is painted under the command, in the same panel.
	m.term.width = 100
	if got := stripANSI(m.renderPermission(100)); !strings.Contains(got, "$ make build") || !strings.Contains(got, want) {
		t.Fatalf("prompt:\n%s", got)
	}
	// A blank line sets the review apart from the command.
	lines := strings.Split(stripANSI(m.renderPermission(100)), "\n")
	for i, l := range lines {
		if strings.Contains(l, "$ make build") {
			if strings.TrimSpace(lines[i+1]) != "" || !strings.Contains(lines[i+2], want) {
				t.Fatalf("review should follow the command after a blank line:\n%s", strings.Join(lines, "\n"))
			}
		}
	}
	select {
	case r := <-replies:
		t.Fatalf("the user has not answered yet: %+v", r)
	default:
	}

	// An error never approves, and says so in the prompt.
	m, _ = reviewModel(t)
	m.handleReviewDone(reviewDoneMsg{id: 1, start: start, verdict: classifier.Verdict{Err: errors.New("boom")}})
	if m.panel.perm == nil || m.panel.perm.review != "Auto review unavailable: boom" {
		t.Fatalf("error should prompt and say why: %+v", m.panel.perm)
	}
}

func TestReviewVerdictForStaleTurnIsDropped(t *testing.T) {
	m, replies := reviewModel(t)
	cmd, handled := m.dispatchTurnMsg(reviewDoneMsg{id: 99, start: turnToolStartMsg{name: tools.Bash, args: bashArgs("make build")},
		verdict: classifier.Verdict{Approved: true}})
	if !handled || cmd != nil || m.panel.perm != nil {
		t.Fatal("a verdict for another turn must change nothing")
	}
	select {
	case r := <-replies:
		t.Fatalf("stale verdict answered the loop: %+v", r)
	default:
	}
}
