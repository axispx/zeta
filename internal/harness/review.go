package harness

import (
	"encoding/json"
	"strings"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/classifier"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
)

// Reviewer returns the session's auto reviewer, or nil when review is off or no
// backend is available. It is built from the live config and client each call,
// so /config and /model changes apply to the next command without a restart.
func (s *Session) Reviewer() *classifier.Reviewer {
	var client classifier.Completer
	if s.Client != nil {
		client = s.Client
	}
	return classifier.New(s.Cfg.Review, client)
}

// ReviewRequest reports what to ask the reviewer about a gated tool call, or
// false when the call must go straight to the user. Only a shell command whose
// parts can be named is reviewed, because the reviewer is asked about the parts
// no rule covers; a command it cannot split (file redirect, substitution,
// subshell) and any part that names a dotenv secret or a path outside the
// workspace keep their prompt, since those are the cases a command's text alone
// does not settle.
func (s *Session) ReviewRequest(name string, args json.RawMessage) (classifier.Request, bool) {
	if name != tools.Bash {
		return classifier.Request{}, false
	}
	command := tools.ArgCommand(args)
	pending, ok := s.Rules.Policy().Pending(tools.Bash, command)
	if !ok || len(pending) == 0 {
		return classifier.Request{}, false
	}
	for _, part := range pending {
		if policy.ProtectedPath(part) {
			return classifier.Request{}, false
		}
	}
	return classifier.Request{
		Command: command,
		Pending: pending,
		Workdir: tools.ArgWorkdir(args),
		Branch:  s.WS.Branch,

		UserRequest: s.lastUserText(),
	}, true
}

// lastUserText is the user's latest message, the only statement of intent the
// reviewer may trust.
func (s *Session) lastUserText() string {
	for i := len(s.History) - 1; i >= 0; i-- {
		if m := s.History[i]; m.Role == ai.RoleUser && strings.TrimSpace(m.Text) != "" {
			return m.Text
		}
	}
	return ""
}
