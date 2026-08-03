package tui

import (
	"github.com/axispx/zeta/internal/tools"
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/prompt"
)

func TestHandlePermissionKey(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		grants: &permission.Session{},
		bottom: bottomSlot{perm: newPermissionPrompt("bash echo", tools.Bash, "")},
		turn:   &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: 'a', Text: "a"}); !ok {
		t.Fatal("expected handled")
	}
	if m.bottom.perm != nil {
		t.Fatal("perm should clear")
	}
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("want allow")
	}

	replies = make(chan agent.Reply, 1)
	m.bottom.perm = newPermissionPrompt("", tools.Bash, "")
	m.turn.reply = replies
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: 's', Text: "s"}); !ok {
		t.Fatal("expected s handled")
	}
	if !m.grants.Granted(tools.Bash) {
		t.Fatal("session grant should stick on harness")
	}
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("want allow")
	}

	replies = make(chan agent.Reply, 1)
	m.bottom.perm = newPermissionPrompt("", tools.Bash, "")
	m.turn.reply = replies
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: 'd', Text: "d"}); !ok {
		t.Fatal("expected d handled")
	}
	if allow := <-replies; allow.Kind != agent.ReplyDeny {
		t.Fatal("want deny")
	}
}

func TestHandlePermissionKeyEditNoSession(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		grants: &permission.Session{},
		bottom: bottomSlot{perm: newPermissionPrompt("", tools.Edit, "a.go")},
		turn:   &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	// [s] is not an option for edit — swallowed, no decision.
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: 's', Text: "s"}); !ok {
		t.Fatal("unknown key still consumed")
	}
	if m.bottom.perm == nil {
		t.Fatal("perm should remain")
	}
	if m.grants.Granted(tools.Edit) {
		t.Fatal("edit must never receive a session grant")
	}
	select {
	case <-replies:
		t.Fatal("should not decide on s")
	default:
	}

	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: 'a', Text: "a"}); !ok {
		t.Fatal("a")
	}
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("want allow")
	}
	if m.grants.Granted(tools.Edit) || m.grants.Granted(tools.Write) {
		t.Fatal("allow once must not grant edit/write")
	}
}

func TestHandlePermissionKeyNavEnter(t *testing.T) {
	// edit has Allow / Deny (2 options)
	replies := make(chan agent.Reply, 1)
	m := Model{
		grants: &permission.Session{},
		bottom: bottomSlot{perm: newPermissionPrompt("", tools.Edit, "")},
		turn:   &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Text: "down"}); !ok {
		t.Fatal("down")
	}
	if m.bottom.perm.list.selected != 1 {
		t.Fatalf("selected=%d", m.bottom.perm.list.selected)
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}); !ok {
		t.Fatal("enter")
	}
	if allow := <-replies; allow.Kind != agent.ReplyDeny {
		t.Fatal("want deny from selection")
	}

	replies = make(chan agent.Reply, 1)
	m.bottom.perm = newPermissionPrompt("", tools.Edit, "")
	m.bottom.perm.list.selected = 1
	m.turn.reply = replies
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Text: "up"}); !ok {
		t.Fatal("up")
	}
	if m.bottom.perm.list.selected != 0 {
		t.Fatalf("selected=%d", m.bottom.perm.list.selected)
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}); !ok {
		t.Fatal("enter allow")
	}
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("want allow from selection")
	}
}

func TestHandlePermissionKeyIdle(t *testing.T) {
	m := Model{}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: 'a', Text: "a"}); ok {
		t.Fatal("should not handle when idle")
	}
}

func TestGapHeightWithPermission(t *testing.T) {
	m := Model{width: 80, bottom: bottomSlot{perm: newPermissionPrompt("", tools.Bash, "")}}
	// blank + panel pad + title + 3 options (bash)
	if h := m.gapHeight(); h < 5 {
		t.Fatalf("gapHeight=%d, want padded options panel", h)
	}
	m.bottom.perm = newPermissionPrompt("", tools.Edit, "")
	// blank + panel pad + title + 2 options (edit)
	if h := m.gapHeight(); h < 4 {
		t.Fatalf("edit gapHeight=%d", h)
	}
}

func TestPermissionHidesInput(t *testing.T) {
	m := testModel()
	m.width = 80
	m.height = 24
	m.bottom.perm = newPermissionPrompt("bash echo", tools.Bash, "")
	m.layout()
	hAsk := m.viewport.Height()
	m.bottom.perm = nil
	m.layout()
	hIdle := m.viewport.Height()
	if hAsk <= hIdle {
		t.Fatalf("hiding input should grow transcript: ask=%d idle=%d", hAsk, hIdle)
	}
}

func TestRenderPermissionVertical(t *testing.T) {
	m := Model{
		width: 80,
		bottom: bottomSlot{
			perm: newPermissionPrompt("create ashish.md", tools.Edit, "ashish.md")},
	}
	out := stripANSI(m.renderPermission(80))
	if !strings.Contains(out, "Edit ashish.md") {
		t.Fatalf("missing title: %q", out)
	}
	if strings.Contains(out, "Permission required") {
		t.Fatalf("no eyebrow: %q", out)
	}
	if strings.Contains(out, "+ hello") || strings.Contains(out, "+hello") {
		t.Fatalf("diff must not live in the prompt: %q", out)
	}
	for _, want := range []string{"Allow", "Deny"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "Allow for session") || strings.Contains(out, "Allow once") {
		t.Fatalf("edit must not offer session grant: %q", out)
	}
}

func TestRenderPermissionBashOptions(t *testing.T) {
	m := Model{
		width: 80,
		bottom: bottomSlot{
			perm: newPermissionPrompt("bash go test", tools.Bash, "")},
	}
	out := stripANSI(m.renderPermission(80))
	for _, want := range []string{"Allow once", "Allow for session", "Deny"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestRenderPermissionWrite(t *testing.T) {
	m := Model{
		width: 80,
		bottom: bottomSlot{
			perm: newPermissionPrompt("write a.txt", tools.Write, "a.txt")},
	}
	out := stripANSI(m.renderPermission(80))
	if !strings.Contains(out, "Write a.txt") {
		t.Fatalf("write title: %q", out)
	}
	if strings.Contains(out, "Allow for session") {
		t.Fatalf("write must not offer session grant: %q", out)
	}
}

func TestRenderPermissionBash(t *testing.T) {
	m := Model{
		width: 80,
		bottom: bottomSlot{
			perm: newPermissionPrompt("bash go test", tools.Bash, "")},
	}
	out := stripANSI(m.renderPermission(80))
	if !strings.Contains(out, "Run this ") || !strings.Contains(out, "bash") || !strings.Contains(out, " command?") {
		t.Fatalf("title: %q", out)
	}
	if strings.Contains(out, "go test") {
		t.Fatalf("command belongs in transcript, not prompt: %q", out)
	}
}

func TestSideEffectToolStartOpensApproval(t *testing.T) {
	diff := "--- a.txt\n+++ a.txt\n@@ -0,0 +1 @@\n+hi\n"
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.width = 80
	m.height = 24
	m.turn = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	cmd := m.handleTurnToolStart(turnToolStartMsg{
		name:   tools.Edit,
		label:  "create a.txt",
		path:   "a.txt",
		detail: diff,
	})
	if len(m.messages) != 1 {
		t.Fatalf("should open tool row: %d", len(m.messages))
	}
	if strings.TrimSpace(m.messages[0].Out) != strings.TrimSpace(diff) {
		t.Fatalf("preview should land on tool Out: %q", m.messages[0].Out)
	}
	if m.bottom.perm == nil || m.bottom.perm.name != "edit" || m.bottom.perm.path != "a.txt" {
		t.Fatalf("perm: %+v", m.bottom.perm)
	}
	out := stripANSI(m.renderPermission(80))
	if strings.Contains(out, "+hi") || strings.Contains(out, "+ hi") {
		t.Fatalf("diff should not be in prompt: %q", out)
	}
	row := stripANSI(renderEditCall(m.messages[0]))
	if !strings.HasPrefix(row, "Creating  a.txt") {
		t.Fatalf("pending verb: %q", row)
	}
	if !strings.Contains(row, "+ hi") && !strings.Contains(row, "+hi") {
		t.Fatalf("transcript row should show diff: %q", row)
	}
	if cmd == nil {
		t.Fatal("want waitTurn cmd")
	}
	select {
	case <-replies:
		t.Fatal("should wait for human decision")
	default:
	}
}

func TestReadToolStartNoDecision(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.turn = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Read, label: "read a.go"})
	if m.bottom.perm != nil {
		t.Fatal("read should not open modal")
	}
	select {
	case d := <-replies:
		t.Fatalf("agent is not waiting; must not send decision: %v", d)
	default:
	}
}

func TestPermissionOptionAt(t *testing.T) {
	vp := viewport.New()
	vp.SetHeight(10)
	m := Model{
		width:    80,
		viewport: vp,
		bottom: bottomSlot{
			perm: newPermissionPrompt("", tools.Bash, "")},
	}
	// gap starts at y=10; blank spacer + panel pad → content at 12.
	// title=12, options 13/14/15 (Allow once / Allow for session / Deny)
	if i := m.permissionOptionAt(2, 13); i != 0 {
		t.Fatalf("opt0: got %d", i)
	}
	if i := m.permissionOptionAt(2, 14); i != 1 {
		t.Fatalf("opt1: got %d", i)
	}
	if i := m.permissionOptionAt(2, 15); i != 2 {
		t.Fatalf("opt2: got %d", i)
	}
	if i := m.permissionOptionAt(2, 16); i != -1 {
		t.Fatalf("past last option should miss: %d", i)
	}
	if i := m.permissionOptionAt(2, 12); i != -1 {
		t.Fatalf("title row should miss: %d", i)
	}

	// edit has only 2 options
	m.bottom.perm = newPermissionPrompt("", tools.Edit, "")
	if i := m.permissionOptionAt(2, 14); i != 1 {
		t.Fatalf("edit deny: got %d", i)
	}
	if i := m.permissionOptionAt(2, 15); i != -1 {
		t.Fatalf("edit has no third option: %d", i)
	}
}

func TestHandlePermissionClick(t *testing.T) {
	vp := viewport.New()
	vp.SetHeight(10)
	replies := make(chan agent.Reply, 1)
	m := Model{
		width:    80,
		viewport: vp,
		grants:   &permission.Session{},
		bottom:   bottomSlot{perm: newPermissionPrompt("", tools.Bash, "")},
		turn:     &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	// y=15 is Deny for bash (3 options)
	if _, ok := m.handlePermissionClick(tea.MouseClickMsg{X: 2, Y: 15, Button: tea.MouseLeft}); !ok {
		t.Fatal("expected click handled")
	}
	if allow := <-replies; allow.Kind != agent.ReplyDeny {
		t.Fatal("want deny")
	}
}

func TestRenderDeniedShell(t *testing.T) {
	out := stripANSI(renderShellCall(Message{
		Role: RoleTool, Text: "bash echo hi", Tool: tools.Bash, Status: ToolDenied, Out: "should hide",
	}))
	if !strings.Contains(out, "denied") {
		t.Fatalf("missing denied: %q", out)
	}
	if strings.Contains(out, "should hide") {
		t.Fatalf("should hide output: %q", out)
	}
}

func TestRenderDeniedEdit(t *testing.T) {
	out := stripANSI(renderEditCall(Message{
		Role: RoleTool, Text: "edit foo.go", Tool: tools.Edit, Status: ToolDenied,
		Out: "--- foo.go\n+++ foo.go\n+x\n",
	}))
	if !strings.Contains(out, "denied") {
		t.Fatalf("missing denied: %q", out)
	}
	if !strings.HasPrefix(out, "Edited") {
		t.Fatalf("denied should use past tense: %q", out)
	}
	if strings.Contains(out, "+x") {
		t.Fatalf("should hide diff: %q", out)
	}
}

func TestSessionGrantSkipsPrompt(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.grants.Grant(tools.Bash)
	m.turn = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash echo"})
	if m.bottom.perm != nil {
		t.Fatal("should not open modal when granted")
	}
	select {
	case d := <-replies:
		t.Fatalf("agent Gate is false; must not send decision: %v", d)
	default:
	}
}

func TestEditAlwaysPromptsEvenAfterBashGrant(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.grants.Grant(tools.Bash)
	m.turn = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Edit, label: "edit a.go", path: "a.go"})
	if m.bottom.perm == nil || m.bottom.perm.name != "edit" {
		t.Fatalf("edit must still prompt: %+v", m.bottom.perm)
	}
	select {
	case <-replies:
		t.Fatal("should wait for human")
	default:
	}
}

func TestActiveGrantsSurviveMode(t *testing.T) {
	m := Model{grants: &permission.Session{}}
	m.grants.Grant(tools.Bash)
	if !m.grants.Granted(tools.Bash) {
		t.Fatal("session grant should stick")
	}
	m.mode = prompt.ModeAsk
	if !m.grants.Granted(tools.Bash) {
		t.Fatal("session grant should survive mode switch")
	}
}

func TestReadToolStartSkipsPrompt(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.turn = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Read, label: "read a.go"})
	if m.bottom.perm != nil {
		t.Fatal("read should not open approval")
	}
	select {
	case d := <-replies:
		t.Fatalf("agent Gate is false; must not send decision: %v", d)
	default:
	}
}

func TestHandlePermissionKeySwallowsUnknown(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		grants: &permission.Session{},
		bottom: bottomSlot{perm: newPermissionPrompt("", tools.Bash, "")},
		turn:   &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Text: "/"}); !ok {
		t.Fatal("unknown keys should be consumed while prompt open")
	}
	if m.bottom.perm == nil {
		t.Fatal("perm should remain")
	}
	select {
	case <-replies:
		t.Fatal("should not decide")
	default:
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Text: "esc"}); ok {
		t.Fatal("esc must fall through to interrupt")
	}
}

func TestFinishTurnDeniesOpenApproval(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.messages = []Message{{Role: RoleTool, Text: "edit a.go", Tool: tools.Edit}}
	m.bottom.perm = newPermissionPrompt("", tools.Edit, "")
	m.turn = &turnSession{
		activeTool: 0,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	m.finishTurn()
	if m.bottom.perm != nil {
		t.Fatal("perm should clear")
	}
	if m.messages[0].Status != ToolDenied {
		t.Fatalf("status=%v want denied", m.messages[0].Status)
	}
	if allow := <-replies; allow.Kind != agent.ReplyDeny {
		t.Fatalf("abandon should deny")
	}
}
