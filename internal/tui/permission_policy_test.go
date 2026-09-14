package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

func TestPermOptionsOrderAndKeys(t *testing.T) {
	var keys, labels []string
	for _, o := range permOptionsFor(tools.Bash, true, "go test", false) {
		keys = append(keys, o.key)
		labels = append(labels, o.label)
	}
	wantKeys := []string{"a", "p", "s", "d"}
	wantLabels := []string{"Allow once", "Always allow `go test`", "Allow for session", "Deny"}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("bash keys=%v want %v", keys, wantKeys)
	}
	if strings.Join(labels, "|") != strings.Join(wantLabels, "|") {
		t.Fatalf("bash labels=%v want %v", labels, wantLabels)
	}

	// There is no always-deny row or hotkey.
	for _, o := range permOptionsFor(tools.Bash, true, "go test", false) {
		if strings.Contains(o.label, "deny ") || o.key == "x" {
			t.Fatalf("no persistent deny row: %+v", o)
		}
	}

	// Without a derivable rule the persist row drops out.
	keys = nil
	for _, o := range permOptionsFor(tools.Bash, false, "", false) {
		keys = append(keys, o.key)
	}
	if strings.Join(keys, ",") != "a,s,d" {
		t.Fatalf("bash no-persist keys=%v", keys)
	}

	// edit/write use file wording.
	keys = nil
	for _, o := range permOptionsFor(tools.Edit, true, "", false) {
		keys = append(keys, o.key)
	}
	if strings.Join(keys, ",") != "a,p,d" {
		t.Fatalf("edit keys=%v", keys)
	}

	keys = nil
	labels = nil
	for _, o := range permOptionsFor(tools.Read, false, "", false) {
		keys = append(keys, o.key)
		labels = append(labels, o.label)
	}
	if strings.Join(keys, ",") != "a,s,d" {
		t.Fatalf("read keys=%v", keys)
	}
	if strings.Join(labels, "|") != "Allow once|Allow this directory for session|Deny" {
		t.Fatalf("read labels=%v", labels)
	}

	keys = nil
	for _, o := range permOptionsFor(tools.Read, true, "", true) {
		keys = append(keys, o.key)
	}
	if strings.Join(keys, ",") != "a,p,d" {
		t.Fatalf("env read keys=%v", keys)
	}
}

func TestAutoDenyNoPanel(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.rules = permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Bash, Command: "rm -rf /", Action: policy.ActionDeny}}})
	m.turn = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash rm -rf /", args: bashArgs("rm -rf /")})
	if m.bottom.perm != nil {
		t.Fatal("auto-deny must not open a panel")
	}
	if len(m.messages) != 1 || m.messages[0].Status != ToolDenied {
		t.Fatalf("tool row should be denied: %+v", m.messages)
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
	if !p.canPersist {
		t.Fatal("bash with command should be persistable")
	}
	out := stripANSI(Model{width: 80, bottom: bottomSlot{perm: p}}.renderPermission(80))
	if !strings.Contains(out, "Always allow `go test`") {
		t.Fatalf("missing always-allow row in %q", out)
	}
	if strings.Contains(out, "Always deny") {
		t.Fatalf("must not offer persistent deny: %q", out)
	}

	// in-tree edit offers persist rows
	pe := newPermissionPrompt("edit a.go", tools.Edit, "a.go")
	pe.setArgs(json.RawMessage(`{"path":"a.go"}`), root)
	if !pe.canPersist {
		t.Fatal("in-tree edit should be persistable")
	}
	out = stripANSI(Model{width: 80, bottom: bottomSlot{perm: pe}}.renderPermission(80))
	if !strings.Contains(out, "Always allow this file") {
		t.Fatalf("edit persist row: %q", out)
	}
}

func TestPromptNoPersistOutOfWorkspace(t *testing.T) {
	root := t.TempDir()
	p := newPermissionPrompt("edit ../x.txt", tools.Edit, "../x.txt")
	p.setArgs(json.RawMessage(`{"path":"../x.txt"}`), root)
	if p.canPersist {
		t.Fatal("out-of-workspace edit must not be persistable")
	}
	out := stripANSI(Model{width: 80, bottom: bottomSlot{perm: p}}.renderPermission(80))
	if strings.Contains(out, "Always") {
		t.Fatalf("out-of-workspace prompt must not offer persist: %q", out)
	}

	pr := newPermissionPrompt("read ../x.txt", tools.Read, "../x.txt")
	pr.setArgs(json.RawMessage(`{"path":"../x.txt"}`), root)
	if pr.canPersist || !pr.outside {
		t.Fatalf("out-of-workspace read: persist=%v outside=%v", pr.canPersist, pr.outside)
	}
	out = stripANSI(Model{width: 80, bottom: bottomSlot{perm: pr}}.renderPermission(80))
	if strings.Contains(out, "Always") {
		t.Fatalf("outside read must not offer persist: %q", out)
	}
	if !strings.Contains(out, "Allow this directory for session") {
		t.Fatalf("outside read should offer directory session grant: %q", out)
	}

	// bash without a command is not persistable either
	pb := newPermissionPrompt("bash", tools.Bash, "")
	pb.setArgs(json.RawMessage(`{}`), root)
	if pb.canPersist {
		t.Fatal("bash with no command must not be persistable")
	}
}

func TestPromptNoPersistChainedCommand(t *testing.T) {
	root := t.TempDir()
	p := newPermissionPrompt("bash go test && rm -rf /", tools.Bash, "")
	p.setArgs(bashArgs("go test && rm -rf /"), root)
	if p.canPersist {
		t.Fatal("chained command must not be rememberable")
	}
	out := stripANSI(Model{width: 80, bottom: bottomSlot{perm: p}}.renderPermission(80))
	if strings.Contains(out, "Always") {
		t.Fatalf("chained command must not offer persist: %q", out)
	}
	// A flag-first command still remembers, but keeps the whole command (no broadening).
	pf := newPermissionPrompt("bash rm -rf /", tools.Bash, "")
	pf.setArgs(bashArgs("rm -rf /"), root)
	if !pf.canPersist || pf.rule.CommandPrefix != "rm -rf /" {
		t.Fatalf("flag-first rule: canPersist=%v rule=%+v", pf.canPersist, pf.rule)
	}
}

func TestPersistAllowWritesRuleAndSkipsSecondPrompt(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.ws = workspace.Context{Abs: root}
	m.turn = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test", args: bashArgs("go test")})
	if m.bottom.perm == nil || !m.bottom.perm.canPersist {
		t.Fatalf("expected a persistable prompt: %+v", m.bottom.perm)
	}
	m.turn.activeTool = -1 // reset the open row for the decision

	m.decidePermission(permission.AllowAlways)
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("allow-always should allow the current call")
	}
	want := policy.Rule{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow}
	if len(m.rules.Policy().Rules) != 1 || m.rules.Policy().Rules[0] != want {
		t.Fatalf("in-memory rules: %+v", m.rules.Policy())
	}
	loaded, err := policy.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Rules) != 1 || loaded.Rules[0] != want {
		t.Fatalf("persisted rules: %+v", loaded)
	}

	// A different command under the same prefix is auto-allowed: no panel, no reply.
	m.turn.activeTool = -1
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test -v", args: bashArgs("go test -v")})
	if m.bottom.perm != nil {
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
	m.ws = workspace.Context{Abs: root}
	m.rules = permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Read, Path: ".env", Action: policy.ActionDeny}}})
	m.turn = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Read, label: "read .env", path: ".env", args: json.RawMessage(`{"path":".env"}`)})
	if m.bottom.perm != nil {
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
	m.ws = workspace.Context{Abs: root}
	m.rules = permission.NewRules(policy.Policy{Rules: []policy.Rule{{Tool: tools.Edit, Path: "a.go", Action: policy.ActionDeny}}})
	m.turn = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Edit, label: "edit a.go", path: "a.go", args: json.RawMessage(`{"path":"a.go"}`)})
	if m.bottom.perm != nil {
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

func TestPersistHotkeyPWritesAllow(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.ws = workspace.Context{Abs: root}
	m.turn = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash go test", args: bashArgs("go test")})
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: 'p', Text: "p"}); !ok {
		t.Fatal("p must be handled while the prompt is open")
	}
	if r := <-replies; r.Kind != agent.ReplyRun {
		t.Fatalf("p should allow this call: %+v", r)
	}
	want := policy.Rule{Tool: tools.Bash, CommandPrefix: "go test", Action: policy.ActionAllow}
	if len(m.rules.Policy().Rules) != 1 || m.rules.Policy().Rules[0] != want {
		t.Fatalf("rules: %+v", m.rules.Policy())
	}
	if m.bottom.perm != nil {
		t.Fatal("prompt should clear")
	}
}

func TestHotkeyXIsInert(t *testing.T) {
	isolateZetaHome(t)
	root := t.TempDir()
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.ws = workspace.Context{Abs: root}
	m.turn = &turnSession{activeTool: -1, ch: make(chan agent.Event), reply: replies, cancel: func() {}}

	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Edit, label: "edit a.go", path: "a.go", args: json.RawMessage(`{"path":"a.go"}`)})
	// x is not an option: swallowed, no decision, no rule.
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: 'x', Text: "x"}); !ok {
		t.Fatal("unknown key still consumed")
	}
	if m.bottom.perm == nil {
		t.Fatal("prompt should remain on an unknown key")
	}
	select {
	case r := <-replies:
		t.Fatalf("x must not decide: %+v", r)
	default:
	}
	if len(m.rules.Policy().Rules) != 0 {
		t.Fatalf("x must not write a rule: %+v", m.rules.Policy())
	}
}
