package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/prompt"
)

func TestHandlePermissionKey(t *testing.T) {
	replies := make(chan bool, 1)
	m := Model{
		grants: &permission.Session{},
		perm:   &permissionPrompt{name: "bash", label: "bash echo"},
		turn:   &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	if !m.handlePermissionKey(tea.KeyPressMsg{Code: 'a', Text: "a"}) {
		t.Fatal("expected handled")
	}
	if m.perm != nil {
		t.Fatal("perm should clear")
	}
	if allow := <-replies; !allow {
		t.Fatal("want allow")
	}

	replies = make(chan bool, 1)
	m.perm = &permissionPrompt{name: "bash"}
	m.turn.reply = replies
	if !m.handlePermissionKey(tea.KeyPressMsg{Code: 's', Text: "s"}) {
		t.Fatal("expected s handled")
	}
	if !m.grants.Granted("bash") {
		t.Fatal("session grant should stick on harness")
	}
	if allow := <-replies; !allow {
		t.Fatal("want allow")
	}

	replies = make(chan bool, 1)
	m.perm = &permissionPrompt{}
	m.turn.reply = replies
	if !m.handlePermissionKey(tea.KeyPressMsg{Code: 'd', Text: "d"}) {
		t.Fatal("expected d handled")
	}
	if allow := <-replies; allow {
		t.Fatal("want deny")
	}
}

func TestHandlePermissionKeyNavEnter(t *testing.T) {
	replies := make(chan bool, 1)
	m := Model{
		perm: &permissionPrompt{selected: 0},
		turn: &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	if !m.handlePermissionKey(tea.KeyPressMsg{Text: "down"}) {
		t.Fatal("down")
	}
	if m.perm.selected != 1 {
		t.Fatalf("selected=%d", m.perm.selected)
	}
	if !m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}) {
		t.Fatal("enter")
	}
	if allow := <-replies; !allow {
		t.Fatal("want allow from selection")
	}
}

func TestHandlePermissionKeyIdle(t *testing.T) {
	m := Model{}
	if m.handlePermissionKey(tea.KeyPressMsg{Code: 'a', Text: "a"}) {
		t.Fatal("should not handle when idle")
	}
}

func TestGapHeightWithPermission(t *testing.T) {
	m := Model{width: 80, perm: &permissionPrompt{name: "bash"}}
	// blank + panel pad + title + 3 options
	if h := m.gapHeight(); h < 5 {
		t.Fatalf("gapHeight=%d, want padded options panel", h)
	}
}

func TestPermissionHidesInput(t *testing.T) {
	m := testModel()
	m.width = 80
	m.height = 24
	m.perm = &permissionPrompt{name: "bash", label: "bash echo"}
	m.layout()
	hAsk := m.viewport.Height()
	m.perm = nil
	m.layout()
	hIdle := m.viewport.Height()
	if hAsk <= hIdle {
		t.Fatalf("hiding input should grow transcript: ask=%d idle=%d", hAsk, hIdle)
	}
}

func TestRenderPermissionVertical(t *testing.T) {
	m := Model{
		width: 80,
		perm: &permissionPrompt{
			name:     "edit",
			label:    "create ashish.md",
			path:     "ashish.md",
			selected: 1,
		},
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
	for _, want := range []string{"Allow once", "Allow for session", "Deny"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestRenderPermissionWrite(t *testing.T) {
	m := Model{
		width: 80,
		perm: &permissionPrompt{
			name:  "write",
			label: "write a.txt",
			path:  "a.txt",
		},
	}
	out := stripANSI(m.renderPermission(80))
	if !strings.Contains(out, "Write a.txt") {
		t.Fatalf("write title: %q", out)
	}
}

func TestRenderPermissionBash(t *testing.T) {
	m := Model{
		width: 80,
		perm: &permissionPrompt{
			name:  "bash",
			label: "bash go test",
		},
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
	replies := make(chan bool, 1)
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
		name:   "edit",
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
	if m.perm == nil || m.perm.name != "edit" || m.perm.path != "a.txt" {
		t.Fatalf("perm: %+v", m.perm)
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
	replies := make(chan bool, 1)
	m := testModel()
	m.turn = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: "read", label: "read a.go"})
	if m.perm != nil {
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
		perm: &permissionPrompt{
			name: "bash",
		},
	}
	// gap starts at y=10; blank spacer + panel pad → content at 12.
	// title=12, options 13/14/15
	if i := m.permissionOptionAt(2, 13); i != 0 {
		t.Fatalf("opt0: got %d", i)
	}
	if i := m.permissionOptionAt(2, 14); i != 1 {
		t.Fatalf("opt1: got %d", i)
	}
	if i := m.permissionOptionAt(2, 15); i != 2 {
		t.Fatalf("opt2: got %d", i)
	}
	if i := m.permissionOptionAt(2, 12); i != -1 {
		t.Fatalf("title row should miss: %d", i)
	}
}

func TestHandlePermissionClick(t *testing.T) {
	vp := viewport.New()
	vp.SetHeight(10)
	replies := make(chan bool, 1)
	m := Model{
		width:    80,
		viewport: vp,
		perm:     &permissionPrompt{name: "bash"},
		turn:     &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	if !m.handlePermissionClick(tea.MouseClickMsg{X: 2, Y: 15, Button: tea.MouseLeft}) {
		t.Fatal("expected click handled")
	}
	if allow := <-replies; allow {
		t.Fatal("want deny")
	}
}

func TestRenderDeniedShell(t *testing.T) {
	out := stripANSI(renderShellCall(Message{
		Role: RoleTool, Text: "bash echo hi", Tool: "bash", Status: ToolDenied, Out: "should hide",
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
		Role: RoleTool, Text: "edit foo.go", Tool: "edit", Status: ToolDenied,
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
	replies := make(chan bool, 1)
	m := testModel()
	m.grants.Grant("bash")
	m.turn = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: "bash", label: "bash echo"})
	if m.perm != nil {
		t.Fatal("should not open modal when granted")
	}
	select {
	case d := <-replies:
		t.Fatalf("agent Gate is false; must not send decision: %v", d)
	default:
	}
}

func TestActiveGrantsSurviveMode(t *testing.T) {
	m := Model{grants: &permission.Session{}}
	m.grants.Grant("bash")
	if !m.grants.Granted("bash") {
		t.Fatal("session grant should stick")
	}
	m.mode = prompt.ModeAsk
	if !m.grants.Granted("bash") {
		t.Fatal("session grant should survive mode switch")
	}
}

func TestHandlePermissionKeySwallowsUnknown(t *testing.T) {
	replies := make(chan bool, 1)
	m := Model{
		perm: &permissionPrompt{},
		turn: &turnSession{reply: replies, activeTool: -1, cancel: func() {}},
	}
	if !m.handlePermissionKey(tea.KeyPressMsg{Text: "/"}) {
		t.Fatal("unknown keys should be consumed while prompt open")
	}
	if m.perm == nil {
		t.Fatal("perm should remain")
	}
	select {
	case <-replies:
		t.Fatal("should not decide")
	default:
	}
	if m.handlePermissionKey(tea.KeyPressMsg{Text: "esc"}) {
		t.Fatal("esc must fall through to interrupt")
	}
}

func TestFinishTurnDeniesOpenApproval(t *testing.T) {
	replies := make(chan bool, 1)
	m := testModel()
	m.messages = []Message{{Role: RoleTool, Text: "edit a.go", Tool: "edit"}}
	m.perm = &permissionPrompt{name: "edit"}
	m.turn = &turnSession{
		activeTool: 0,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	m.finishTurn()
	if m.perm != nil {
		t.Fatal("perm should clear")
	}
	if m.messages[0].Status != ToolDenied {
		t.Fatalf("status=%v want denied", m.messages[0].Status)
	}
	if allow := <-replies; allow {
		t.Fatalf("abandon should deny")
	}
}
