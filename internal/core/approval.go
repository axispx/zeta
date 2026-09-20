package core

import (
	"encoding/json"

	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/tools"
)

// Approval is the harness view of one gated tool call: the derived permission
// call (the rule a persist decision writes, the directory a session grant
// covers) and the decisions a client may offer, in display order. Clients
// render it and dispatch one of Choices; they do not re-derive it.
type Approval struct {
	Call permission.Call
	// Env marks an in-workspace dotenv read: the file prompt, not a directory grant.
	Env bool
	// Choices are the offered decisions, most permissive first.
	Choices []permission.Decision
}

// ApprovalFor derives the approval view of a gated tool call.
func ApprovalFor(root, name string, args json.RawMessage) Approval {
	call := permission.CallFor(root, name, args)
	env := permission.EnvFile(call.Match.Path)
	return Approval{Call: call, Env: env, Choices: approvalChoices(name, call.Persist, env)}
}

// approvalChoices is the decisions a prompt may offer. bash and outside reads
// get a session grant; an in-workspace dotenv read or a bash command get an
// "always allow" row when a rule can be remembered. edit/write stay
// allow-or-deny, so every diff is reviewed and no rule pre-approves a mutation.
func approvalChoices(tool string, canPersist, env bool) []permission.Decision {
	if tool == tools.Read {
		if !env {
			// Outside read: a directory-scoped session grant.
			return []permission.Decision{permission.AllowOnce, permission.AllowSession, permission.Deny}
		}
		choices := []permission.Decision{permission.AllowOnce}
		if canPersist {
			choices = append(choices, permission.AllowAlways)
		}
		return append(choices, permission.Deny)
	}
	if permission.SessionGrantable(tool) {
		choices := []permission.Decision{permission.AllowOnce}
		if canPersist {
			choices = append(choices, permission.AllowAlways)
		}
		return append(choices, permission.AllowSession, permission.Deny)
	}
	// edit/write: once-only, every diff reviewed.
	return []permission.Decision{permission.AllowOnce, permission.Deny}
}
