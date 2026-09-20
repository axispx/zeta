package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/core"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

func TestPermOptionsOrderAndLabels(t *testing.T) {
	root := t.TempDir()
	labelsOf := func(tool string, args json.RawMessage) []string {
		var labels []string
		for _, o := range permOptions(tool, core.ApprovalFor(root, tool, args)) {
			labels = append(labels, o.label)
		}
		return labels
	}

	// Row order is what the digit shortcuts select, so it is part of the contract.
	labels := labelsOf(tools.Bash, bashArgs("go test"))
	wantLabels := []string{"Allow once", "Always allow `go test`", "Allow for session", "Deny"}
	if strings.Join(labels, "|") != strings.Join(wantLabels, "|") {
		t.Fatalf("bash labels=%v want %v", labels, wantLabels)
	}

	// There is no persistent deny row.
	for _, o := range permOptions(tools.Bash, core.ApprovalFor(root, tools.Bash, bashArgs("go test"))) {
		if strings.Contains(o.label, "deny ") {
			t.Fatalf("no persistent deny row: %+v", o)
		}
	}

	// Without a derivable rule the persist row drops out.
	labels = labelsOf(tools.Bash, json.RawMessage(`{}`))
	if strings.Join(labels, "|") != "Allow once|Allow for session|Deny" {
		t.Fatalf("bash no-persist labels=%v", labels)
	}

	// edit/write are allow/deny only — persist is deliberately ignored.
	labels = labelsOf(tools.Edit, json.RawMessage(`{"path":"a.go"}`))
	if strings.Join(labels, "|") != "Allow|Deny" {
		t.Fatalf("edit labels=%v", labels)
	}

	labels = labelsOf(tools.Read, json.RawMessage(`{"path":"../x.txt"}`))
	if strings.Join(labels, "|") != "Allow once|Allow this directory for session|Deny" {
		t.Fatalf("read labels=%v", labels)
	}

	labels = labelsOf(tools.Read, json.RawMessage(`{"path":".env"}`))
	if strings.Join(labels, "|") != "Allow|Always allow this file|Deny" {
		t.Fatalf("env read labels=%v", labels)
	}
}

func TestAutoDenyNoPanel(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.Rules = permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Bash, Command: "rm -rf /", Action: policy.ActionDeny}}})
	m.turn.current = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash rm -rf /", args: bashArgs("rm -rf /")})
	if m.panel.perm != nil {
		t.Fatal("auto-deny must not open a panel")
	}
	if len(m.transcript.messages) != 1 || m.transcript.messages[0].Status != ToolDenied {
		t.Fatalf("tool row should be denied: %+v", m.transcript.messages)
	}
	select {
	case r := <-replies:
		if r.Kind != agent.ReplyDeny || r.Reason != "denied by permission policy" {
			t.Fatalf("reply=%+v", r)
		}
	default:
		t.Fatal("expected an auto-deny reply")
	}
}

func TestPromptPersistRows(t *testing.T) {
	root := t.TempDir()

	// bash with a command offers persist rows
	p := newPermissionPrompt("bash go test", tools.Bash, "")
	p.setArgs(bashArgs("go test"), root)
	if !p.appr.Call.Persist {
		t.Fatal("bash with command should be persistable")
	}
	out := stripANSI(Model{term: term{width: 80}, panel: panel{perm: p}}.renderPermission(80))
	if !strings.Contains(out, "Always allow `go test`") {
		t.Fatalf("missing always-allow row in %q", out)
	}
	if strings.Contains(out, "Always deny") {
		t.Fatalf("must not offer persistent deny: %q", out)
	}

	// in-tree edit is still once-only: no persist row even though the path is
	// rememberable.
	pe := newPermissionPrompt("edit a.go", tools.Edit, "a.go")
	pe.setArgs(json.RawMessage(`{"path":"a.go"}`), root)
	out = stripANSI(Model{term: term{width: 80}, panel: panel{perm: pe}}.renderPermission(80))
	if strings.Contains(out, "Always") {
		t.Fatalf("edit must not offer a persist row: %q", out)
	}
}

func TestPromptNoPersistOutOfWorkspace(t *testing.T) {
	root := t.TempDir()
	p := newPermissionPrompt("edit ../x.txt", tools.Edit, "../x.txt")
	p.setArgs(json.RawMessage(`{"path":"../x.txt"}`), root)
	if p.appr.Call.Persist {
		t.Fatal("out-of-workspace edit must not be persistable")
	}
	out := stripANSI(Model{term: term{width: 80}, panel: panel{perm: p}}.renderPermission(80))
	if strings.Contains(out, "Always") {
		t.Fatalf("out-of-workspace prompt must not offer persist: %q", out)
	}

	pr := newPermissionPrompt("read ../x.txt", tools.Read, "../x.txt")
	pr.setArgs(json.RawMessage(`{"path":"../x.txt"}`), root)
	if pr.appr.Call.Persist || !pr.appr.Call.Outside {
		t.Fatalf("out-of-workspace read: persist=%v outside=%v", pr.appr.Call.Persist, pr.appr.Call.Outside)
	}
	out = stripANSI(Model{term: term{width: 80}, panel: panel{perm: pr}}.renderPermission(80))
	if strings.Contains(out, "Always") {
		t.Fatalf("outside read must not offer persist: %q", out)
	}
	if !strings.Contains(out, "Allow this directory for session") {
		t.Fatalf("outside read should offer directory session grant: %q", out)
	}

	// bash without a command is not persistable either
	pb := newPermissionPrompt("bash", tools.Bash, "")
	pb.setArgs(json.RawMessage(`{}`), root)
	if pb.appr.Call.Persist {
		t.Fatal("bash with no command must not be persistable")
	}
}

func TestPromptNoPersistChainedCommand(t *testing.T) {
	root := t.TempDir()
	p := newPermissionPrompt("bash go test && rm -rf /", tools.Bash, "")
	p.setArgs(bashArgs("go test && rm -rf /"), root)
	if p.appr.Call.Persist {
		t.Fatal("chained command must not be rememberable")
	}
	out := stripANSI(Model{term: term{width: 80}, panel: panel{perm: p}}.renderPermission(80))
	if strings.Contains(out, "Always") {
		t.Fatalf("chained command must not offer persist: %q", out)
	}
	// A flag-first command still remembers, but keeps the whole command (no broadening).
	pf := newPermissionPrompt("bash rm -rf /", tools.Bash, "")
	pf.setArgs(bashArgs("rm -rf /"), root)
	if !pf.appr.Call.Persist || pf.appr.Call.Rule.CommandPrefix != "rm -rf /" {
		t.Fatalf("flag-first rule: canPersist=%v rule=%+v", pf.appr.Call.Persist, pf.appr.Call.Rule)
	}
}

func TestPersistAllowWritesRuleAndSkipsSecondPrompt(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: root}
	m.turn.current = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test", args: bashArgs("go test")})
	if m.panel.perm == nil || !m.panel.perm.appr.Call.Persist {
		t.Fatalf("expected a persistable prompt: %+v", m.panel.perm)
	}
	m.turn.current.activeTool = -1 // reset the open row for the decision

	m.decidePermission(permission.AllowAlways)
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("allow-always should allow the current call")
	}
	want := policy.Rule{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow}
	if len(m.session.Rules.Policy().Rules) != 1 || m.session.Rules.Policy().Rules[0] != want {
		t.Fatalf("in-memory rules: %+v", m.session.Rules.Policy())
	}
	loaded, err := policy.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Rules) != 1 || loaded.Rules[0] != want {
		t.Fatalf("persisted rules: %+v", loaded)
	}

	// A different command under the same prefix is auto-allowed: no panel, no reply.
	m.turn.current.activeTool = -1
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test -v", args: bashArgs("go test -v")})
	if m.panel.perm != nil {
		t.Fatal("prefix rule should skip the prompt")
	}
	select {
	case r := <-replies:
		t.Fatalf("agent gate is false for allowed call; must not reply: %+v", r)
	default:
	}
}

func TestHandWrittenReadDenyAutoDenies(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: root}
	m.session.Rules = permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Read, Path: ".env", Action: policy.ActionDeny}}})
	m.turn.current = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Read, label: "read .env", path: ".env", args: json.RawMessage(`{"path":".env"}`)})
	if m.panel.perm != nil {
		t.Fatal("read deny rule should skip the prompt")
	}
	select {
	case r := <-replies:
		if r.Kind != agent.ReplyDeny || r.Reason != "denied by permission policy" {
			t.Fatalf("reply=%+v", r)
		}
	default:
		t.Fatal("expected auto-deny reply")
	}
}

func TestHandWrittenDenyRuleAutoDenies(t *testing.T) {
	// Deny rules are not created from the prompt; a hand-edited file still
	// auto-denies with no panel.
	isolateZetaHome(t)
	root := t.TempDir()
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: root}
	m.session.Rules = permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Edit, Path: "a.go", Action: policy.ActionDeny}}})
	m.turn.current = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Edit, label: "edit a.go", path: "a.go", args: json.RawMessage(`{"path":"a.go"}`)})
	if m.panel.perm != nil {
		t.Fatal("deny rule should skip the prompt")
	}
	select {
	case r := <-replies:
		if r.Kind != agent.ReplyDeny || r.Reason != "denied by permission policy" {
			t.Fatalf("reply=%+v", r)
		}
	default:
		t.Fatal("expected auto-deny reply")
	}
}

func TestPersistRowWritesAllow(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: root}
	m.turn.current = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test", args: bashArgs("go test")})
	// Row 2 is "always allow" for a persistable command; it must be confirmed.
	pressPermRow(t, &m, permission.AllowAlways)
	if r := <-replies; r.Kind != agent.ReplyRun {
		t.Fatalf("always-allow should allow this call: %+v", r)
	}
	want := policy.Rule{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow}
	if len(m.session.Rules.Policy().Rules) != 1 || m.session.Rules.Policy().Rules[0] != want {
		t.Fatalf("rules: %+v", m.session.Rules.Policy())
	}
	if m.panel.perm != nil {
		t.Fatal("prompt should clear")
	}
}

func TestStrayLetterIsInert(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: root}
	m.turn.current = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Edit, label: "edit a.go", path: "a.go", args: json.RawMessage(`{"path":"a.go"}`)})
	// Letters are not shortcuts: swallowed, no decision, no rule. Typing a
	// message over an open prompt must never answer it.
	for _, key := range []string{"a", "d", "p", "s", "x"} {
		if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: rune(key[0]), Text: key}); !ok {
			t.Fatalf("%q must be consumed", key)
		}
	}
	if m.panel.perm == nil {
		t.Fatal("prompt should remain on a letter")
	}
	select {
	case r := <-replies:
		t.Fatalf("a letter must not decide: %+v", r)
	default:
	}
	if len(m.session.Rules.Policy().Rules) != 0 {
		t.Fatalf("a letter must not write a rule: %+v", m.session.Rules.Policy())
	}
}

// Edit/write prompts are once-only: exactly allow and deny. No session grant
// (that is bash / outside reads) and no "always allow this file" row.
func TestEditPromptOnlyAllowAndDeny(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name   string
		tool   string
		path   string
		args   string
		labels []string
	}{
		{"edit", tools.Edit, "a.go", `{"path":"a.go"}`, []string{"Allow", "Deny"}},
		{"write", tools.Write, "a.go", `{"path":"a.go"}`, []string{"Allow", "Deny"}},
		{"edit outside", tools.Edit, "../x.txt", `{"path":"../x.txt"}`, []string{"Allow", "Deny"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPermissionPrompt(tc.name, tc.tool, tc.path)
			p.setArgs(json.RawMessage(tc.args), root)
			var labels []string
			for _, o := range p.opts {
				if o.decide == permission.AllowSession {
					t.Fatalf("edit/write must not offer a session grant: %+v", o)
				}
				labels = append(labels, o.label)
			}
			if strings.Join(labels, "|") != strings.Join(tc.labels, "|") {
				t.Fatalf("labels=%v want %v", labels, tc.labels)
			}
			out := stripANSI(Model{term: term{width: 80}, panel: panel{perm: p}}.renderPermission(80))
			if !strings.Contains(out, "Allow") || !strings.Contains(out, "Deny") {
				t.Fatalf("prompt must offer allow and deny: %q", out)
			}
			if strings.Contains(out, "Always") {
				t.Fatalf("prompt must not offer an always-allow row: %q", out)
			}
			if strings.Contains(out, "session") {
				t.Fatalf("prompt must not offer a session grant: %q", out)
			}
		})
	}
}
