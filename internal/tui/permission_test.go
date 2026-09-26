package tui

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/harness"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/styles"
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
	replies := make(chan harness.Reply, 1)
	m := Model{
		session: harness.Session{Grants: &permission.Session{}},
		panel:   panel{perm: newPermissionPrompt("bash echo", tools.Bash, "")},
		turn:    turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	pressPermRow(t, &m, permission.AllowOnce)
	if m.panel.perm != nil {
		t.Fatal("perm should clear")
	}
	if allow := <-replies; allow.Kind == harness.ReplyDeny {
		t.Fatal("want allow")
	}

	replies = make(chan harness.Reply, 1)
	m.panel.perm = newPermissionPrompt("", tools.Bash, "")
	m.panel.perm.setArgs(policy.Policy{}, bashArgs("echo"), t.TempDir())
	m.turn.current.reply = replies
	pressPermRow(t, &m, permission.AllowSession)
	if !m.session.Grants.CmdGranted("echo") {
		t.Fatal("session grant should stick on harness")
	}
	// The grant is the command on screen, not every bash command.
	if m.session.Grants.CmdGranted("rm -rf /") {
		t.Fatal("session grant must not cover other commands")
	}
	if allow := <-replies; allow.Kind == harness.ReplyDeny {
		t.Fatal("want allow")
	}

	replies = make(chan harness.Reply, 1)
	m.panel.perm = newPermissionPrompt("", tools.Bash, "")
	m.turn.current.reply = replies
	pressPermRow(t, &m, permission.Deny)
	if allow := <-replies; allow.Kind != harness.ReplyDeny {
		t.Fatal("want deny")
	}
}

func TestHandlePermissionKeyEditNoSession(t *testing.T) {
	replies := make(chan harness.Reply, 1)
	m := Model{
		session: harness.Session{Grants: &permission.Session{}},
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
	if m.session.Grants.CmdGranted("a.go") {
		t.Fatal("edit must never receive a session grant")
	}
	select {
	case <-replies:
		t.Fatal("should not decide on an out-of-range row number")
	default:
	}

	pressPermRow(t, &m, permission.AllowOnce)
	if allow := <-replies; allow.Kind == harness.ReplyDeny {
		t.Fatal("want allow")
	}
	if m.session.Grants.CmdGranted("a.go") {
		t.Fatal("allow once must not grant anything")
	}
}

func TestHandlePermissionKeyNavEnter(t *testing.T) {
	// edit has Allow / Deny (2 options)
	replies := make(chan harness.Reply, 1)
	m := Model{
		session: harness.Session{Grants: &permission.Session{}},
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
	if allow := <-replies; allow.Kind != harness.ReplyDeny {
		t.Fatal("want deny from selection")
	}

	replies = make(chan harness.Reply, 1)
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
	if allow := <-replies; allow.Kind == harness.ReplyDeny {
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
	replies := make(chan harness.Reply, 1)
	m := Model{
		session: harness.Session{Grants: &permission.Session{}},
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
	if r := <-replies; r.Kind != harness.ReplyDeny || r.Reason != "wrong7" {
		t.Fatalf("reply=%+v", r)
	}
}

// TestDenyReasonEdges: typing elsewhere is still swallowed; an arrow hands the
// keys back to the list; an empty reason denies plainly.
func TestDenyReasonEdges(t *testing.T) {
	newModel := func() (*Model, chan harness.Reply) {
		replies := make(chan harness.Reply, 1)
		m := &Model{
			session: harness.Session{Grants: &permission.Session{}},
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
	if r := <-replies; r.Kind != harness.ReplyDeny || r.Reason != "the user denied this call" {
		t.Fatalf("plain deny: %+v", r)
	}
}

// TestDenyReasonRendering: the Deny row shows the typed reason with a caret
// while it owns the keys, and the placeholder before anything is typed.
func TestDenyReasonRendering(t *testing.T) {
	m := Model{term: term{width: 80}, panel: panel{perm: newPermissionPrompt("", tools.Edit, "a.go")}}
	base := stripANSI(m.renderPermission(80))
	if !strings.Contains(base, "No, and tell zeta what to do differently") {
		t.Fatalf("deny row missing: %q", base)
	}
	if !strings.Contains(base, "(esc)") {
		t.Fatalf("deny row must name the Esc key: %q", base)
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
	if m.renderInput() != "" {
		t.Fatal("an open prompt must replace the input")
	}
	// The prompt takes the gap slot above the (hidden) input.
	if h := m.gapHeight(); h < 6 {
		t.Fatalf("gapHeight=%d, want the prompt panel", h)
	}
	m.panel.perm = nil
	if m.renderInput() == "" {
		t.Fatal("input must come back once the prompt closes")
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
	if !strings.Contains(out, "Would you like to make the following edit?") {
		t.Fatalf("missing question: %q", out)
	}
	// The target is repeated in the prompt, not left to the transcript row.
	if !strings.Contains(out, "ashish.md") {
		t.Fatalf("missing payload: %q", out)
	}
	if strings.Contains(out, "Permission required") {
		t.Fatalf("no eyebrow: %q", out)
	}
	if strings.Contains(out, "+ hello") || strings.Contains(out, "+hello") {
		t.Fatalf("diff must not live in the prompt: %q", out)
	}
	for _, want := range []string{"Yes, proceed", "No, and tell zeta what to do differently"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if !strings.Contains(out, permFooter) {
		t.Fatalf("missing key legend: %q", out)
	}
	if strings.Contains(out, "in this session") {
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
	for _, want := range []string{"Yes, proceed", "in this session", "No, and tell zeta what to do differently"} {
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
	if !strings.Contains(out, "Would you like to write the following file?") || !strings.Contains(out, "a.txt") {
		t.Fatalf("write title: %q", out)
	}
	if strings.Contains(out, "in this session") {
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
	m.panel.perm.setArgs(policy.Policy{}, bashArgs("go test"), t.TempDir())
	out := stripANSI(m.renderPermission(80))
	if !strings.Contains(out, "Would you like to run the following command?") {
		t.Fatalf("title: %q", out)
	}
	// The prompt quotes the command itself: the panel is the decision surface, so
	// approving must not depend on a transcript row that may be scrolled away.
	if !strings.Contains(out, "$ go test") {
		t.Fatalf("command payload missing: %q", out)
	}
}

func TestSideEffectToolStartOpensApproval(t *testing.T) {
	diff := "--- a.txt\n+++ a.txt\n@@ -0,0 +1 @@\n+hi\n"
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.term.width = 80
	m.term.height = 24
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
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

func TestHandlePermissionClick(t *testing.T) {
	vp := viewport.New()
	vp.SetHeight(10)
	replies := make(chan harness.Reply, 1)
	m := Model{
		term: term{
			width: 80,
		},
		transcript: transcript{viewport: vp},
		session:    harness.Session{Grants: &permission.Session{}},
		panel:      panel{perm: newPermissionPrompt("", tools.Bash, "")},
		turn:       turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	// y=15 is Deny for bash (3 options)
	if _, ok := m.handlePermissionClick(tea.MouseClickMsg{X: 2, Y: 15, Button: tea.MouseLeft}); !ok {
		t.Fatal("expected click handled")
	}
	if allow := <-replies; allow.Kind != harness.ReplyDeny {
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
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.session.Grants.GrantCmd("echo")
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash echo", args: bashArgs("echo")})
	if m.panel.perm != nil {
		t.Fatal("should not open modal for the granted command")
	}
	select {
	case d := <-replies:
		t.Fatalf("agent Gate is false; must not send decision: %v", d)
	default:
	}
}

// A grant for one command must leave every other command prompting.
func TestSessionGrantDoesNotCoverOtherCommands(t *testing.T) {
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.session.Grants.GrantCmd("echo")
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{name: tools.Bash, label: "bash rm -rf /", args: bashArgs("rm -rf /")})
	if m.panel.perm == nil {
		t.Fatal("an unrelated command must still prompt")
	}
}

func TestEditAlwaysPromptsEvenAfterBashGrant(t *testing.T) {
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.session.Grants.GrantCmd("echo")
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
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
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
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
	m := Model{session: harness.Session{Grants: &permission.Session{}}}
	m.session.Grants.GrantCmd("echo")
	if !m.session.Grants.CmdGranted("echo") {
		t.Fatal("session grant should stick")
	}
	m.session.Mode = prompt.ModeAsk
	if !m.session.Grants.CmdGranted("echo") {
		t.Fatal("session grant should survive mode switch")
	}
}

func TestReadToolStartSkipsPrompt(t *testing.T) {
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
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
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
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
	if !strings.Contains(out, "Would you like to read the following file?") || !strings.Contains(out, ".env") {
		t.Fatalf("title: %q", out)
	}
	if strings.Contains(out, "in this session") {
		t.Fatalf("env must not offer directory grant: %q", out)
	}
	if !strings.Contains(out, "Yes, and don't ask again for this file") {
		t.Fatalf("in-workspace env should persist: %q", out)
	}
	select {
	case <-replies:
		t.Fatal("should wait for human")
	default:
	}
}

func TestReadOutsideOpensApproval(t *testing.T) {
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
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
	if !strings.Contains(out, "Would you like to access the following directory?") {
		t.Fatalf("title: %q", out)
	}
	if !strings.Contains(out, "outside workspace") {
		t.Fatalf("outside marker: %q", out)
	}
	for _, want := range []string{"Yes, proceed", "Yes, and don't ask again for this directory in this session", "No, and tell zeta what to do differently"} {
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
	replies := make(chan harness.Reply, 1)
	outer := t.TempDir()
	root := t.TempDir()
	oneA, _ := json.Marshal(map[string]string{"path": filepath.Join(outer, "one", "a.txt")})
	oneB, _ := json.Marshal(map[string]string{"path": filepath.Join(outer, "one", "b.txt")})
	twoC, _ := json.Marshal(map[string]string{"path": filepath.Join(outer, "two", "c.txt")})

	m := testModel()
	m.session.WS = workspace.Context{Abs: root}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
		reply:      replies,
		cancel:     func() {},
	}
	_ = m.handleTurnToolStart(turnToolStartMsg{
		name: tools.Read, label: "read a.txt", path: filepath.Join(outer, "one", "a.txt"),
		args: oneA,
	})
	pressPermRow(t, &m, permission.AllowSession)
	if !m.session.Grants.DirGranted(permission.CallFor(policy.Policy{}, root, tools.Read, oneA)) {
		t.Fatal("directory grant should stick")
	}
	if m.session.Grants.CmdGranted("a.txt") {
		t.Fatal("must not command-grant read")
	}
	if allow := <-replies; allow.Kind == harness.ReplyDeny {
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
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.session.Mode = prompt.ModeAsk
	m.session.WS = workspace.Context{Abs: t.TempDir()}
	m.turn.current = &turnSession{
		activeTool: -1,
		ch:         make(chan harness.Event),
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
	replies := make(chan harness.Reply, 1)
	m := Model{
		session: harness.Session{Grants: &permission.Session{}},
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
	if r := <-replies; r.Kind != harness.ReplyDeny {
		t.Fatalf("esc should deny: %+v", r)
	}
}

// TestPermissionEscDeniesNotCancels: Esc answers the prompt ("no") and leaves
// the turn running — only Ctrl+C aborts it.
func TestPermissionEscDeniesNotCancels(t *testing.T) {
	replies := make(chan harness.Reply, 1)
	cancelled := false
	m := testModel()
	m.transcript.messages = []Message{{Role: RoleTool, Text: "edit a.go", Tool: tools.Edit}}
	m.panel.perm = newPermissionPrompt("", tools.Edit, "a.go")
	m.turn.current = &turnSession{
		activeTool: 0,
		ch:         make(chan harness.Event),
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
	if r := <-replies; r.Kind != harness.ReplyDeny || r.Reason != "the user denied this call" {
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
	replies := make(chan harness.Reply, 1)
	m := Model{
		session: harness.Session{Grants: &permission.Session{}},
		panel:   panel{perm: newPermissionPrompt("", tools.Edit, "a.go")},
		turn:    turn{current: &turnSession{reply: replies, activeTool: -1, cancel: func() {}}},
	}
	selectDenyRow(t, &m)
	pressPermKey(t, &m, "n")
	pressPermKey(t, &m, "o")
	if _, ok := m.handlePermissionKey(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"}); !ok {
		t.Fatal("esc should be consumed")
	}
	if r := <-replies; r.Kind != harness.ReplyDeny || r.Reason != "no" {
		t.Fatalf("reply=%+v", r)
	}
}

func TestFinishTurnDeniesOpenApproval(t *testing.T) {
	replies := make(chan harness.Reply, 1)
	m := testModel()
	m.transcript.messages = []Message{{Role: RoleTool, Text: "edit a.go", Tool: tools.Edit}}
	m.panel.perm = newPermissionPrompt("", tools.Edit, "")
	m.turn.current = &turnSession{
		activeTool: 0,
		ch:         make(chan harness.Event),
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
	if allow := <-replies; allow.Kind != harness.ReplyDeny {
		t.Fatalf("abandon should deny")
	}
}

// bgSeq extracts the background SGR sequence from rendered output, or "".
func bgSeq(s string) string {
	i := strings.Index(s, "[48;")
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], "m")
	if j < 0 {
		return ""
	}
	return s[i-1 : i+j+1]
}

// The prompt payload — the command or the path — must be painted on the panel
// fill. A style without it (the bare diff/text styles) punches the terminal
// background through the panel and reads as a highlight band across the prompt.
func TestPermissionPayloadCarriesPanelFill(t *testing.T) {
	chrome := styles.NewChrome(lipgloss.Color("235"), true)
	ink := chrome.OverlayInk()
	fill := bgSeq(ink.Gap.Render("x"))
	if fill == "" {
		t.Fatal("test needs a chrome with a panel fill set")
	}
	for _, tc := range []struct {
		name string
		got  string
	}{
		{"command", codeCommand(ink, &permissionPrompt{name: tools.Bash, command: "go test ./..."})},
		{"path", codePath(ink, "src/a.go", false)},
		{"path outside", codePath(ink, "/etc/passwd", true)},
	} {
		if !strings.Contains(tc.got, fill) {
			t.Errorf("%s payload must carry the panel fill %q: %q", tc.name, fill, tc.got)
		}
	}

	// The payload is ordinary row text: not dimmed, and the `$` marker is quieter
	// than the command rather than brighter.
	if got, want := codeCommand(ink, &permissionPrompt{command: "ls"}), ink.Hint.Render("$ ")+ink.Row.Render("ls"); got != want {
		t.Errorf("command payload = %q, want %q", got, want)
	}
	if got, want := codePath(ink, "a.go", true), ink.Row.Render("a.go")+ink.Warn.Render(" (outside workspace)"); got != want {
		t.Errorf("outside path payload = %q, want %q", got, want)
	}

	// The key legend is a hint, not content.
	m := Model{term: term{width: 80, chrome: chrome}, panel: panel{perm: newPermissionPrompt("go test", tools.Bash, "")}}
	m.panel.perm.setArgs(policy.Policy{}, bashArgs("go test"), t.TempDir())
	out := m.renderPermission(80)
	legend := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, permFooter) {
			legend = line
		}
	}
	if legend == "" {
		t.Fatalf("missing legend: %q", out)
	}
	if !strings.Contains(legend, ink.Hint.Render(permFooter)) {
		t.Errorf("legend must be dim italic hint text: %q", legend)
	}
}
