package tui

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/agent"
	"github.com/axispx/zeta/internal/core"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/tools"
	"github.com/axispx/zeta/internal/workspace"
)

// pressPermRow answers the open prompt with the row number of decision d.
// Letters are not shortcuts, so tests decide the way the UI does: move by
// number (or ↑/↓), then Enter.
func pressPermRow(t *testing.T, m *Model, d permission.Decision) {
	t.Helper()
	p := m.panel.perm
	if p == nil {
		t.Fatal("no permission prompt open")
	}
	for i, o := range p.opts {
		if o.decide != d {
			continue
		}
		key := strconv.Itoa(i + 1)
		if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: rune(key[0]), Text: key}); !ok {
			t.Fatalf("row number %s for %v not handled", key, d)
		}
		if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}); !ok {
			t.Fatalf("enter after row number %s not handled", key)
		}
		return
	}
	t.Fatalf("prompt offers no %v row: %+v", d, p.opts)
}

func TestHandlePermissionKey(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		session: core.Session{Grants: &permission.Session{}},
		panel:   panel{perm: newPermissionPrompt("bash echo", tools.Bash, "")},
		turn:    turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	pressPermRow(t, &m, permission.AllowOnce)
	if m.panel.perm != nil {
		t.Fatal("perm should clear")
	}
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("want allow")
	}

	replies = make(chan agent.Reply, 1)
	m.panel.perm = newPermissionPrompt("", tools.Bash, "")
	m.panel.perm.setArgs(bashArgs("echo"), t.TempDir())
	m.turn.current.reply = replies
	pressPermRow(t, &m, permission.AllowSession)
	if !m.session.Grants.Granted(tools.Bash) {
		t.Fatal("session grant should stick on harness")
	}
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("want allow")
	}

	replies = make(chan agent.Reply, 1)
	m.panel.perm = newPermissionPrompt("", tools.Bash, "")
	m.turn.current.reply = replies
	pressPermRow(t, &m, permission.Deny)
	if allow := <-replies; allow.Kind != agent.ReplyDeny {
		t.Fatal("want deny")
	}
}

func TestHandlePermissionKeyEditNoSession(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		session: core.Session{Grants: &permission.Session{}},
		panel:   panel{perm: newPermissionPrompt("", tools.Edit, "a.go")},
		turn:    turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	// Edit offers exactly two rows: a digit past the last row is swallowed
	// without deciding, and no session grant can come of it.
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: '9', Text: "9"}); !ok {
		t.Fatal("out-of-range row number still consumed")
	}
	if m.panel.perm == nil {
		t.Fatal("perm should remain")
	}
	if m.session.Grants.Granted(tools.Edit) {
		t.Fatal("edit must never receive a session grant")
	}
	select {
	case <-replies:
		t.Fatal("should not decide on an out-of-range row number")
	default:
	}

	pressPermRow(t, &m, permission.AllowOnce)
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("want allow")
	}
	if m.session.Grants.Granted(tools.Edit) || m.session.Grants.Granted(tools.Write) {
		t.Fatal("allow once must not grant edit/write")
	}
}

func TestHandlePermissionKeyNavEnter(t *testing.T) {
	// edit has Allow / Deny (2 options)
	replies := make(chan agent.Reply, 1)
	m := Model{
		session: core.Session{Grants: &permission.Session{}},
		panel:   panel{perm: newPermissionPrompt("", tools.Edit, "")},
		turn:    turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Text: "down"}); !ok {
		t.Fatal("down")
	}
	if m.panel.perm.list.selected != 1 {
		t.Fatalf("selected=%d", m.panel.perm.list.selected)
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}); !ok {
		t.Fatal("enter")
	}
	if allow := <-replies; allow.Kind != agent.ReplyDeny {
		t.Fatal("want deny from selection")
	}

	replies = make(chan agent.Reply, 1)
	m.panel.perm = newPermissionPrompt("", tools.Edit, "")
	m.panel.perm.list.selected = 1
	m.turn.current.reply = replies
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Text: "up"}); !ok {
		t.Fatal("up")
	}
	if m.panel.perm.list.selected != 0 {
		t.Fatalf("selected=%d", m.panel.perm.list.selected)
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

// pressPermKey presses a printable key the way the terminal delivers it.
func pressPermKey(t *testing.T, m *Model, s string) {
	t.Helper()
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: rune(s[0]), Text: s}); !ok {
		t.Fatalf("key %q not handled", s)
	}
}

// selectDenyRow moves the cursor onto the Deny row with its row number, the way
// the UI does (a number only moves; Enter confirms).
func selectDenyRow(t *testing.T, m *Model) {
	t.Helper()
	p := m.panel.perm
	if p == nil {
		t.Fatal("no permission prompt open")
	}
	i := p.reasonRow()
	if i < 0 {
		t.Fatalf("prompt offers no deny row: %+v", p.opts)
	}
	pressPermKey(t, m, strconv.Itoa(i+1))
	if p.list.selected != i {
		t.Fatalf("selected=%d want deny row %d", p.list.selected, i)
	}
}

// TestDenyReasonTypeToFocus: with Deny selected, typing is the denial's reason
// and never decides; Enter then denies with that reason.
func TestDenyReasonTypeToFocus(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		session: core.Session{Grants: &permission.Session{}},
		panel:   panel{perm: newPermissionPrompt("", tools.Edit, "a.go")},
		turn:    turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	selectDenyRow(t, &m)
	select {
	case r := <-replies:
		t.Fatalf("selecting the deny row must not decide: %+v", r)
	default:
	}

	// Letters and digits are the reason's text, not row jumps or a decision.
	for _, s := range []string{"w", "r", "o", "n", "g", "7"} {
		pressPermKey(t, &m, s)
	}
	if !m.panel.perm.typing {
		t.Fatal("typing on deny should claim the keys")
	}
	if got := m.panel.perm.reason; got != "wrong7" {
		t.Fatalf("reason=%q", got)
	}
	select {
	case r := <-replies:
		t.Fatalf("typing must not decide: %+v", r)
	default:
	}

	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}); !ok {
		t.Fatal("enter submit not handled")
	}
	if m.panel.perm != nil {
		t.Fatal("perm should clear after denying")
	}
	if r := <-replies; r.Kind != agent.ReplyDeny || r.Reason != "wrong7" {
		t.Fatalf("reply=%+v", r)
	}
}

// TestDenyReasonEdges: typing elsewhere is still swallowed; an arrow hands the
// keys back to the list; an empty reason denies plainly.
func TestDenyReasonEdges(t *testing.T) {
	newModel := func() (*Model, chan agent.Reply) {
		replies := make(chan agent.Reply, 1)
		m := &Model{
			session: core.Session{Grants: &permission.Session{}},
			panel:   panel{perm: newPermissionPrompt("", tools.Edit, "a.go")},
			turn:    turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
		}
		return m, replies
	}

	// With Allow selected, a letter is still swallowed (no reason field).
	m, _ := newModel()
	pressPermKey(t, m, "x")
	if m.panel.perm.typing || m.panel.perm.reason != "" {
		t.Fatalf("typing off the deny row must not start a reason: %+v", m.panel.perm)
	}

	// ↑ exits the field (keeping the text) and moves the list.
	m, _ = newModel()
	selectDenyRow(t, m)
	pressPermKey(t, m, "x")
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Text: "up"}); !ok {
		t.Fatal("up should be consumed")
	}
	if m.panel.perm.typing {
		t.Fatal("an arrow should hand keys back to the list")
	}
	if m.panel.perm.reason != "x" {
		t.Fatalf("reason should survive: %q", m.panel.perm.reason)
	}
	if want := m.panel.perm.reasonRow() - 1; m.panel.perm.list.selected != want {
		t.Fatalf("selection should move up: %d want %d", m.panel.perm.list.selected, want)
	}

	// Empty field + Enter = plain deny (generic reason).
	m, replies := newModel()
	selectDenyRow(t, m)
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}); !ok {
		t.Fatal("enter on the deny row not handled")
	}
	if r := <-replies; r.Kind != agent.ReplyDeny || r.Reason != "the user denied this call" {
		t.Fatalf("plain deny: %+v", r)
	}
}

// TestDenyReasonRendering: the Deny row shows the typed reason with a caret
// while it owns the keys, and the placeholder before anything is typed.
func TestDenyReasonRendering(t *testing.T) {
	m := Model{term: term{width: 80}, panel: panel{perm: newPermissionPrompt("", tools.Edit, "a.go")}}
	base := stripANSI(m.renderPermission(80))
	if !strings.Contains(base, "Deny") {
		t.Fatalf("deny row missing: %q", base)
	}
	if strings.Contains(base, optionCaret) || strings.Contains(base, denyReasonPlaceholder) {
		t.Fatalf("no field before typing: %q", base)
	}

	selectDenyRow(t, &m)
	pressPermKey(t, &m, "t")
	out := stripANSI(m.renderPermission(80))
	if !strings.Contains(out, "t"+optionCaret) {
		t.Fatalf("live reason with caret: %q", out)
	}
}

func TestGapHeightWithPermission(t *testing.T) {
	m := Model{term: term{width: 80}, panel: panel{perm: newPermissionPrompt("", tools.Bash, "")}}
	// blank + panel pad + title + 3 options (bash)
	if h := m.gapHeight(); h < 5 {
		t.Fatalf("gapHeight=%d, want padded options panel", h)
	}
	m.panel.perm = newPermissionPrompt("", tools.Edit, "")
	// blank + panel pad + title + 2 options (edit)
	if h := m.gapHeight(); h < 4 {
		t.Fatalf("edit gapHeight=%d", h)
	}
}

func TestPermissionHidesInput(t *testing.T) {
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	m.panel.perm = newPermissionPrompt("bash echo", tools.Bash, "")
	m.layout()
	hAsk := m.transcript.viewport.Height()
	m.panel.perm = nil
	m.layout()
	hIdle := m.transcript.viewport.Height()
	if hAsk <= hIdle {
		t.Fatalf("hiding input should grow transcript: ask=%d idle=%d", hAsk, hIdle)
	}
}

func TestRenderPermissionVertical(t *testing.T) {
	m := Model{
		term: term{
			width: 80,
		},
		panel: panel{
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
		term: term{
			width: 80,
		},
		panel: panel{
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
		term: term{
			width: 80,
		},
		panel: panel{
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
		term: term{
			width: 80,
		},
		panel: panel{
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
	m.term.width = 80
	m.term.height = 24
	m.turn.current = &turnSession{
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
	if len(m.transcript.messages) != 1 {
		t.Fatalf("should open tool row: %d", len(m.transcript.messages))
	}
	if strings.TrimSpace(m.transcript.messages[0].Out) != strings.TrimSpace(diff) {
		t.Fatalf("preview should land on tool Out: %q", m.transcript.messages[0].Out)
	}
	if m.panel.perm == nil || m.panel.perm.name != "edit" || m.panel.perm.path != "a.txt" {
		t.Fatalf("perm: %+v", m.panel.perm)
	}
	out := stripANSI(m.renderPermission(80))
	if strings.Contains(out, "+hi") || strings.Contains(out, "+ hi") {
		t.Fatalf("diff should not be in prompt: %q", out)
	}
	row := stripANSI(renderEditCall(m.transcript.messages[0]))
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
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Read, label: "read a.go", args: json.RawMessage(`{"path":"a.go"}`)})
	if m.panel.perm != nil {
		t.Fatal("read should not open modal")
	}
	select {
	case d := <-replies:
		t.Fatalf("agent is not waiting; must not send decision: %v", d)
	default:
	}
}

func TestHandlePermissionClick(t *testing.T) {
	vp := viewport.New()
	vp.SetHeight(10)
	replies := make(chan agent.Reply, 1)
	m := Model{
		term: term{
			width: 80,
		},
		transcript: transcript{viewport: vp},
		session:    core.Session{Grants: &permission.Session{}},
		panel:      panel{perm: newPermissionPrompt("", tools.Bash, "")},
		turn:       turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
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
	m.session.Grants.Grant(tools.Bash)
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash echo"})
	if m.panel.perm != nil {
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
	m.session.Grants.Grant(tools.Bash)
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Edit, label: "edit a.go", path: "a.go"})
	if m.panel.perm == nil || m.panel.perm.name != "edit" {
		t.Fatalf("edit must still prompt: %+v", m.panel.perm)
	}
	select {
	case <-replies:
		t.Fatal("should wait for human")
	default:
	}
}

func TestEditOutsideWorkspacePromptFlagsOutside(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Edit, label: "edit ../x.txt", path: "../x.txt",
		args: json.RawMessage(`{"path":"../x.txt"}`),
	})
	if m.panel.perm == nil || !m.panel.perm.appr.Call.Outside {
		t.Fatalf("outside edit must flag prompt: %+v", m.panel.perm)
	}
	if view := m.renderPermission(80); !strings.Contains(view, "outside workspace") {
		t.Fatalf("prompt must show outside marker: %s", view)
	}

	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Edit, label: "edit a.go", path: "a.go",
		args: json.RawMessage(`{"path":"a.go"}`),
	})
	if m.panel.perm == nil || m.panel.perm.appr.Call.Outside {
		t.Fatalf("in-tree edit must not be flagged: %+v", m.panel.perm)
	}
	if view := m.renderPermission(80); strings.Contains(view, "outside workspace") {
		t.Fatalf("in-tree prompt must not show marker: %s", view)
	}
}

func TestActiveGrantsSurviveMode(t *testing.T) {
	m := Model{session: core.Session{Grants: &permission.Session{}}}
	m.session.Grants.Grant(tools.Bash)
	if !m.session.Grants.Granted(tools.Bash) {
		t.Fatal("session grant should stick")
	}
	m.session.Mode = prompt.ModeAsk
	if !m.session.Grants.Granted(tools.Bash) {
		t.Fatal("session grant should survive mode switch")
	}
}

func TestReadToolStartSkipsPrompt(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Read, label: "read a.go", args: json.RawMessage(`{"path":"a.go"}`)})
	if m.panel.perm != nil {
		t.Fatal("read should not open approval")
	}
	select {
	case d := <-replies:
		t.Fatalf("agent Gate is false; must not send decision: %v", d)
	default:
	}
}

func TestEnvReadOpensApproval(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Read, label: "read .env", path: ".env",
		args: json.RawMessage(`{"path":".env"}`),
	})
	if m.panel.perm == nil || !m.panel.perm.appr.Env || m.panel.perm.appr.Call.Outside {
		t.Fatalf("env read must open file prompt: %+v", m.panel.perm)
	}
	out := stripANSI(m.renderPermission(80))
	if !strings.Contains(out, "Read ") || !strings.Contains(out, ".env") {
		t.Fatalf("title: %q", out)
	}
	if strings.Contains(out, "Allow this directory") {
		t.Fatalf("env must not offer directory grant: %q", out)
	}
	if !strings.Contains(out, "Always allow this file") {
		t.Fatalf("in-workspace env should persist: %q", out)
	}
	select {
	case <-replies:
		t.Fatal("should wait for human")
	default:
	}
}

func TestReadOutsideOpensApproval(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Read, label: "read ../x.txt", path: "../x.txt",
		args: json.RawMessage(`{"path":"../x.txt"}`),
	})
	if m.panel.perm == nil || m.panel.perm.name != tools.Read || !m.panel.perm.appr.Call.Outside {
		t.Fatalf("outside read must open prompt: %+v", m.panel.perm)
	}
	out := stripANSI(m.renderPermission(80))
	if !strings.Contains(out, "Access ") {
		t.Fatalf("title: %q", out)
	}
	if !strings.Contains(out, "outside workspace") {
		t.Fatalf("outside marker: %q", out)
	}
	for _, want := range []string{"Allow once", "Allow this directory for session", "Deny"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "Always") {
		t.Fatalf("must not persist: %q", out)
	}
	select {
	case <-replies:
		t.Fatal("should wait for human")
	default:
	}
}

func TestReadOutsideSessionGrantSkipsLater(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	outer := t.TempDir()
	root := t.TempDir()
	oneA, _ := json.Marshal(map[string]string{"path": filepath.Join(outer, "one", "a.txt")})
	oneB, _ := json.Marshal(map[string]string{"path": filepath.Join(outer, "one", "b.txt")})
	twoC, _ := json.Marshal(map[string]string{"path": filepath.Join(outer, "two", "c.txt")})

	m := testModel()
	m.session.WS = workspace.Context{Abs: root}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Read, label: "read a.txt", path: filepath.Join(outer, "one", "a.txt"),
		args: oneA,
	})
	pressPermRow(t, &m, permission.AllowSession)
	if !m.session.Grants.DirGranted(permission.CallFor(root, tools.Read, oneA)) {
		t.Fatal("directory grant should stick")
	}
	if m.session.Grants.Granted(tools.Read) {
		t.Fatal("must not class-grant read")
	}
	if allow := <-replies; allow.Kind == agent.ReplyDeny {
		t.Fatal("want allow")
	}

	m.turn.current.activeTool = -1
	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Read, label: "read b.txt", path: filepath.Join(outer, "one", "b.txt"),
		args: oneB,
	})
	if m.panel.perm != nil {
		t.Fatal("sibling in the same directory should skip")
	}
	select {
	case d := <-replies:
		t.Fatalf("agent gate is false; must not reply: %v", d)
	default:
	}

	for _, name := range []string{".env", ".env.local"} {
		env, _ := json.Marshal(map[string]string{"path": filepath.Join(outer, "one", name)})
		m.turn.current.activeTool = -1
		_ = m.handleTurnToolStart(turnToolStartMsg{
			name: tools.Read, label: "read " + name, path: filepath.Join(outer, "one", name),
			args: env,
		})
		if m.panel.perm == nil || !m.panel.perm.appr.Env {
			t.Fatalf("directory grant must still prompt %s: %+v", name, m.panel.perm)
		}
		m.panel.clear()
	}

	m.turn.current.activeTool = -1
	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Read, label: "read c.txt", path: filepath.Join(outer, "two", "c.txt"),
		args: twoC,
	})
	if m.panel.perm == nil {
		t.Fatal("a different directory must still prompt")
	}

	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Edit, label: "edit a.go", path: "a.go",
		args: json.RawMessage(`{"path":"a.go"}`),
	})
	if m.panel.perm == nil || m.panel.perm.name != tools.Edit {
		t.Fatalf("read grant must not skip edit: %+v", m.panel.perm)
	}
}

func TestReadOutsidePromptsInAskMode(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.session.Mode = prompt.ModeAsk
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Read, label: "read ../x.txt", path: "../x.txt",
		args: json.RawMessage(`{"path":"../x.txt"}`),
	})
	if m.panel.perm == nil {
		t.Fatal("ask mode must still prompt outside reads")
	}
}

// TestHandlePermissionKeySwallowsUnknown: while the prompt owns the input, keys
// that are not nav / row numbers / Enter are consumed but decide nothing.
func TestHandlePermissionKeySwallowsUnknown(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		session: core.Session{Grants: &permission.Session{}},
		panel:   panel{perm: newPermissionPrompt("", tools.Bash, "")},
		turn:    turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	for _, key := range []tea.KeyPressMsg{{Text: "/"}, {Code: 'a', Text: "a"}, {Code: 'x', Text: "x"}} {
		if _, ok := m.handlePermissionKey(key); !ok {
			t.Fatalf("unknown keys should be consumed while prompt open: %q", key.String())
		}
	}
	if m.panel.perm == nil {
		t.Fatal("perm should remain")
	}
	// Swallowed means swallowed: the prompt starts on Allow, so nothing typed
	// here becomes a deny reason either.
	if m.panel.perm.typing || m.panel.perm.reason != "" {
		t.Fatalf("stray keys must not start a reason: %+v", m.panel.perm)
	}
	select {
	case <-replies:
		t.Fatal("should not decide")
	default:
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Text: "esc"}); !ok {
		t.Fatal("esc should be handled by the prompt")
	}
	if r := <-replies; r.Kind != agent.ReplyDeny {
		t.Fatalf("esc should deny: %+v", r)
	}
}

// TestPermissionEscDeniesNotCancels: Esc answers the prompt ("no") and leaves
// the turn running — only Ctrl+C aborts it.
func TestPermissionEscDeniesNotCancels(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	cancelled := false
	m := testModel()
	m.transcript.messages = []Message{{Role: RoleTool, Text: "edit a.go", Tool: tools.Edit}}
	m.panel.perm = newPermissionPrompt("", tools.Edit, "a.go")
	m.turn.current = &turnSession{
		activeTool: 0,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() { cancelled = true },
	}
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"}); !ok {
		t.Fatal("esc should be consumed by the prompt")
	}
	if m.panel.perm != nil {
		t.Fatal("prompt should close")
	}
	if cancelled || m.turn.current == nil {
		t.Fatal("esc must not cancel the turn")
	}
	if r := <-replies; r.Kind != agent.ReplyDeny || r.Reason != "the user denied this call" {
		t.Fatalf("reply=%+v", r)
	}
	for _, msg := range m.transcript.messages {
		if msg.Text == turnCancelledText {
			t.Fatal("esc must not append Cancelled")
		}
	}
}

// TestPermissionEscTakesTypedReason: Esc denies with the reason already typed.
func TestPermissionEscTakesTypedReason(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := Model{
		session: core.Session{Grants: &permission.Session{}},
		panel:   panel{perm: newPermissionPrompt("", tools.Edit, "a.go")},
		turn:    turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	selectDenyRow(t, &m)
	pressPermKey(t, &m, "n")
	pressPermKey(t, &m, "o")
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"}); !ok {
		t.Fatal("esc should be consumed")
	}
	if r := <-replies; r.Kind != agent.ReplyDeny || r.Reason != "no" {
		t.Fatalf("reply=%+v", r)
	}
}

func TestFinishTurnDeniesOpenApproval(t *testing.T) {
	replies := make(chan agent.Reply, 1)
	m := testModel()
	m.transcript.messages = []Message{{Role: RoleTool, Text: "edit a.go", Tool: tools.Edit}}
	m.panel.perm = newPermissionPrompt("", tools.Edit, "")
	m.turn.current = &turnSession{
		activeTool: 0,
		ch:         make(chan agent.Event),
		reply:      replies,
		cancel:     func() {},
	}
	m.finishTurn()
	if m.panel.perm != nil {
		t.Fatal("perm should clear")
	}
	if m.transcript.messages[0].Status != ToolDenied {
		t.Fatalf("status=%v want denied", m.transcript.messages[0].Status)
	}
	if allow := <-replies; allow.Kind != agent.ReplyDeny {
		t.Fatalf("abandon should deny")
	}
}
