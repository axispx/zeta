// Package core is the UI-agnostic runtime for zeta.
//
// It owns every decision the harness must make while a turn runs — whether a
// tool call needs approval, which kind, and how interactive tools reach the
// user — and assembles each model request (tool set, system prompt, mode
// instructions, trailing context) without importing any UI framework. The TUI
// (and future web/desktop shells) renders core's decisions and reports the
// outcomes back.
//
// The decision vocabulary itself (permission.Decision, permission.Call) already
// lives outside the UI; core reuses it rather than inventing parallel types.
package core

import (
	"encoding/json"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
)

// WaitKind is the single harness-wait classifier for Gate and tool-start UI.
type WaitKind int

const (
	// WaitNone runs the tool without a harness decision.
	WaitNone WaitKind = iota
	// WaitPermission asks the user (approval prompt).
	WaitPermission
	// WaitInteractive opens UI for an interactive tool (e.g. ask_user).
	WaitInteractive
	// WaitAutoDeny rejects without prompting (policy deny).
	WaitAutoDeny
)

// Classify reports whether the harness must decide before a tool runs, and how.
// Interactive tools always wait; otherwise permission.Classify decides run / ask /
// deny (policy rules first, then session grants). Gate and the tool-start UI both
// use this — do not re-branch the policy elsewhere.
func Classify(rules *permission.Rules, grants *permission.Session, root, name string, args json.RawMessage) WaitKind {
	if tools.Interactive(name) {
		return WaitInteractive
	}
	switch permission.Classify(rules, grants, root, name, args) {
	case policy.Deny:
		return WaitAutoDeny
	case policy.Ask:
		return WaitPermission
	default:
		return WaitNone
	}
}

// Gate is the agent-side gate: it waits exactly when the harness will handle
// the tool start. It closes over the live rules/grants holders, so a mid-turn
// rule persist is visible to both sides of the decision.
func Gate(rules *permission.Rules, grants *permission.Session, root string) func(string, json.RawMessage) bool {
	return func(name string, args json.RawMessage) bool {
		return Classify(rules, grants, root, name, args) != WaitNone
	}
}

// DecidePermission applies an approval decision to the session and returns the
// reply the agent is waiting on: it records session grants, persists the rule an
// "always allow" writes, and allows or denies the call. The rule is persisted
// with policy.Add and swapped in place, so the agent's Gate sees it mid-turn.
// A rule that cannot be saved is reported but does not change the decision.
func (s *Session) DecidePermission(d permission.Decision, call permission.Call) (agent.Reply, error) {
	var persistErr error
	switch d {
	case permission.AllowSession:
		if call.Match.Tool == tools.Read {
			s.Grants.GrantDir(call.Dir)
		} else {
			s.Grants.Grant(call.Match.Tool)
		}
	case permission.AllowAlways:
		if call.Persist {
			pol, err := policy.Add(call.Rule)
			if err != nil {
				persistErr = err
			} else {
				s.Rules.Replace(pol)
			}
		}
	}
	if d == permission.Deny {
		return agent.DenyTool(), persistErr
	}
	return agent.RunTool(), persistErr
}
