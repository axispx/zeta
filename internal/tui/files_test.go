package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/workspace"
)

func TestAtTokenAtCursor(t *testing.T) {
	// Helper: place cursor at end of s unless │ marks a caret (stripped).
	parse := func(s string) (val string, line, col int) {
		val = strings.ReplaceAll(s, "│", "")
		// Find caret line/col.
		caret := strings.Index(s, "│")
		if caret < 0 {
			// default: end of string
			lines := strings.Split(val, "\n")
			line = len(lines) - 1
			col = len([]rune(lines[line]))
			return val, line, col
		}
		before := s[:caret]
		line = strings.Count(before, "\n")
		last := before
		if i := strings.LastIndexByte(before, '\n'); i >= 0 {
			last = before[i+1:]
		}
		col = len([]rune(strings.ReplaceAll(last, "│", "")))
		return val, line, col
	}

	tests := []struct {
		name      string
		in        string
		wantOK    bool
		wantQuery string
	}{
		{"bare at", "@│", true, ""},
		{"query", "@mo│", true, "mo"},
		{"mid sentence", "see @bar/baz│ please", true, "bar/baz"},
		{"cursor mid token", "see @ba│r/baz", true, "bar/baz"},
		{"email", "user@host│.com", false, ""},
		{"no at", "hello│", false, ""},
		{"after space", "@file │", false, ""},
		{"multiline", "line1\n@mod│", true, "mod"},
		{"slash still text", "fix @internal/tui/mo│", true, "internal/tui/mo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, line, col := parse(tt.in)
			tok, ok := atTokenAtCursor(val, line, col)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v want %v (val=%q line=%d col=%d tok=%+v)", ok, tt.wantOK, val, line, col, tok)
			}
			if ok && tok.query != tt.wantQuery {
				t.Fatalf("query=%q want %q", tok.query, tt.wantQuery)
			}
			if ok && !strings.HasPrefix(val[tok.start:tok.end], "@") {
				t.Fatalf("range %q does not start with @", val[tok.start:tok.end])
			}
		})
	}
}

func TestCursorByteOffsetRoundTrip(t *testing.T) {
	val := "ab\ncafé"
	for line := 0; line < 2; line++ {
		lines := strings.Split(val, "\n")
		for col := 0; col <= len([]rune(lines[line])); col++ {
			off, ok := cursorByteOffset(val, line, col)
			if !ok {
				t.Fatalf("offset %d,%d", line, col)
			}
			gl, gc := lineColAtByte(val, off)
			if gl != line || gc != col {
				t.Fatalf("roundtrip %d,%d → off %d → %d,%d", line, col, off, gl, gc)
			}
		}
	}
}

func TestSyncFileOverlayListOnceAndInsert(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("fix @mo")
	ta.MoveToEnd()
	m := Model{
		textarea: ta,
		ws:       workspace.Context{Abs: t.TempDir()},
	}
	cmd := m.syncOverlay()
	if m.overlay.mode != overlayFiles {
		t.Fatalf("mode=%v", m.overlay.mode)
	}
	if m.overlay.files.query != "mo" {
		t.Fatalf("query=%q", m.overlay.files.query)
	}
	if cmd == nil {
		t.Fatal("expected list cmd")
	}
	// Same query / still loading: no second list.
	if cmd2 := m.syncOverlay(); cmd2 != nil {
		t.Fatal("duplicate list while loading")
	}

	// Inventory arrives once; filter is sync.
	m.applyFileListMsg(fileListMsg{
		seq:   m.overlay.files.seq,
		paths: []string{"internal/tui/model.go", "internal/tui/mainview.go", "README.md"},
	})
	if m.overlay.files.loading || m.overlay.files.all == nil {
		t.Fatalf("loading=%v all=%v", m.overlay.files.loading, m.overlay.files.all)
	}
	if len(m.overlay.files.matches) == 0 || m.overlay.files.matches[0] != "internal/tui/model.go" {
		t.Fatalf("filtered=%v", m.overlay.files.matches)
	}
	// Stale seq ignored.
	m.applyFileListMsg(fileListMsg{seq: m.overlay.files.seq - 1, paths: []string{"x"}})
	if len(m.overlay.files.all) != 3 {
		t.Fatalf("stale applied: %v", m.overlay.files.all)
	}

	// Query refine: sync refilter, no new list cmd.
	m.textarea.SetValue("fix @main")
	m.textarea.MoveToEnd()
	if cmd3 := m.syncOverlay(); cmd3 != nil {
		t.Fatal("refilter should not re-list")
	}
	if m.overlay.files.query != "main" {
		t.Fatalf("query=%q", m.overlay.files.query)
	}
	if len(m.overlay.files.matches) != 1 || m.overlay.files.matches[0] != "internal/tui/mainview.go" {
		t.Fatalf("refilter=%v", m.overlay.files.matches)
	}

	m.overlay.selected = 0
	m.insertFileMention()
	if m.overlay.mode != overlayOff {
		t.Fatal("overlay should close")
	}
	if got := m.textarea.Value(); got != "fix @internal/tui/mainview.go " {
		t.Fatalf("value=%q", got)
	}
}

func TestSubmitInputInsertsFileNotSend(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("@x")
	ta.MoveToEnd()
	m := Model{textarea: ta, cfg: testClientCfg()}
	m.applyClient()
	m.overlay.mode = overlayFiles
	m.overlay.files.matches = []string{"a.go"}
	m.overlay.files.query = "x"
	// Production path: handleOverlayKey consumes Enter when there is a selection.
	if _, ok := m.handleOverlayKey(teaKeyEnter()); !ok {
		t.Fatal("enter should insert")
	}
	if m.turn != nil {
		t.Fatal("started turn")
	}
	if got := m.textarea.Value(); got != "@a.go " {
		t.Fatalf("value=%q", got)
	}
}

func TestEnterEmptyFileOverlaySubmits(t *testing.T) {
	// No matches: overlay is not showing; Enter goes straight to submit.
	ta := textarea.New()
	ta.SetValue("hello @zzzz")
	ta.MoveToEnd()
	m := Model{textarea: ta, cfg: testClientCfg()}
	m.applyClient()
	m.overlay.mode = overlayFiles
	m.overlay.files.matches = nil
	m.overlay.files.all = []string{"a.go"}
	m.overlay.files.query = "zzzz"

	if m.overlay.showing() {
		t.Fatal("empty matches should hide overlay")
	}
	if _, ok := m.handleOverlayKey(teaKeyEnter()); ok {
		t.Fatal("hidden overlay should not consume enter")
	}
	// Inventory kept for sync refilter when the query changes.
	if len(m.overlay.files.all) != 1 {
		t.Fatalf("inventory dropped: %v", m.overlay.files.all)
	}
	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	if m.textarea.Value() != "" {
		t.Fatalf("input not cleared: %q", m.textarea.Value())
	}
	if m.turn == nil {
		t.Fatal("expected turn started")
	}
}

func TestEmptyFileMatchesRefilterShowsAgain(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("@zzzz")
	ta.MoveToEnd()
	m := Model{
		textarea: ta,
		ws:       workspace.Context{Abs: t.TempDir()},
	}
	m.overlay.mode = overlayFiles
	m.overlay.files.all = []string{"a.go", "b.md"}
	m.overlay.files.query = "zzzz"
	m.refilterFiles()
	if m.overlay.showing() || len(m.overlay.files.matches) != 0 {
		t.Fatalf("want hidden empty, got showing=%v matches=%v", m.overlay.showing(), m.overlay.files.matches)
	}

	m.textarea.SetValue("@a")
	m.textarea.MoveToEnd()
	if cmd := m.syncOverlay(); cmd != nil {
		t.Fatal("should not re-list; inventory present")
	}
	if !m.overlay.showing() {
		t.Fatal("expected list after refilter")
	}
	if len(m.overlay.files.matches) != 1 || m.overlay.files.matches[0] != "a.go" {
		t.Fatalf("matches=%v", m.overlay.files.matches)
	}
}

func TestSlashWinsOverAt(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/cle")
	m := Model{textarea: ta}
	_ = m.syncOverlay()
	if m.overlay.mode != overlayCommands {
		t.Fatalf("mode=%v", m.overlay.mode)
	}
}

func TestFileOverlayFloatsWithoutReplacingStatus(t *testing.T) {
	m := testModel()
	m.width = 60
	m.spinner = spinner.New(spinner.WithSpinner(spinner.MiniDot))
	if idle := m.gapHeight(); idle != 1 {
		t.Fatalf("idle gapHeight=%d want 1", idle)
	}
	// Turn running: status gap stays busy; overlay is floating (not in gapHeight).
	m.turn = &turnSession{streaming: true, activeTool: -1}
	m.overlay.mode = overlayFiles
	m.overlay.files.matches = []string{"a.go", "b.go", "c.go"}
	if got := m.gapHeight(); got != busyStatusRows {
		t.Fatalf("gapHeight=%d want busy %d", got, busyStatusRows)
	}
	if ov := m.renderOverlay(m.width); ov == "" {
		t.Fatal("expected file overlay body")
	}
	// Idle + overlay: blank gap stays reserved (no transcript jump).
	m.turn = nil
	if got := m.gapHeight(); got != 1 {
		t.Fatalf("idle+overlay gapHeight=%d want 1", got)
	}
}

func TestCloseFileOverlayKeepsDraft(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("keep @x")
	m := Model{textarea: ta}
	m.overlay.mode = overlayFiles
	m.overlay.files.matches = []string{"a.go"}
	m.closeOverlay()
	if m.overlay.mode != overlayOff {
		t.Fatal("not cleared")
	}
	if m.textarea.Value() != "keep @x" {
		t.Fatalf("draft wiped: %q", m.textarea.Value())
	}
}

func TestCancelOverlayKeepsFileDraftWipesSlash(t *testing.T) {
	// @ mention: cancel keeps composer.
	ta := textarea.New()
	ta.SetValue("draft @f")
	m := Model{textarea: ta}
	m.overlay.mode = overlayFiles
	m.overlay.files.matches = []string{"a.go"}
	m.cancelOverlay()
	if m.overlay.mode != overlayOff {
		t.Fatal("file overlay still up")
	}
	if m.textarea.Value() != "draft @f" {
		t.Fatalf("file draft wiped: %q", m.textarea.Value())
	}

	// Slash owns input: cancel wipes composer.
	ta2 := textarea.New()
	ta2.SetValue("/cle")
	m2 := Model{textarea: ta2}
	m2.overlay.mode = overlayCommands
	m2.overlay.cmds = []command{{name: "/clear", desc: "start a new session"}}
	m2.cancelOverlay()
	if m2.overlay.mode != overlayOff {
		t.Fatal("command overlay still up")
	}
	if m2.textarea.Value() != "" {
		t.Fatalf("slash query not wiped: %q", m2.textarea.Value())
	}
}

func TestTryInterruptFileOverlayKeepsDraft(t *testing.T) {
	m := testModel()
	m.textarea.SetValue("draft @f")
	m.overlay.mode = overlayFiles
	m.overlay.files.matches = []string{"a.go"}
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.overlay.mode != overlayOff {
		t.Fatal("overlay still up")
	}
	if m.textarea.Value() != "draft @f" {
		t.Fatalf("draft=%q", m.textarea.Value())
	}
}

func TestTryInterruptHiddenFileOverlayKeepsDraft(t *testing.T) {
	// Empty matches → not showing, but mode is still armed; Esc must not cancel a turn.
	m := testModel()
	m.textarea.SetValue("draft @zzz")
	m.turn = &turnSession{streaming: true, activeTool: -1}
	m.overlay.mode = overlayFiles
	m.overlay.files.all = []string{"a.go"}
	m.overlay.files.matches = nil
	if m.overlay.showing() {
		t.Fatal("expected hidden")
	}
	if !m.tryInterrupt() {
		t.Fatal("expected interrupt")
	}
	if m.overlay.mode != overlayOff {
		t.Fatal("mode should clear")
	}
	if m.turn == nil {
		t.Fatal("turn should still be running")
	}
	if m.textarea.Value() != "draft @zzz" {
		t.Fatalf("draft=%q", m.textarea.Value())
	}
}

func TestCommandOverlayEnterViaHandleOverlayKey(t *testing.T) {
	// Enter on a skill fills; does not submit through submitInput.
	ta := textarea.New()
	ta.SetValue("/rev")
	m := Model{textarea: ta}
	_ = m.syncOverlay()
	for i, c := range m.overlay.cmds {
		if c.name == "/review" {
			m.overlay.selected = i
			break
		}
	}
	cmd, ok := m.handleOverlayKey(teaKeyEnter())
	if !ok {
		t.Fatal("enter should commit skill fill")
	}
	if cmd != nil {
		t.Fatal("skill fill should not return a cmd")
	}
	if got := m.textarea.Value(); got != "/review " {
		t.Fatalf("value=%q", got)
	}
}

// teaKeyEnter is a plain Enter press for overlay key tests.
func teaKeyEnter() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter}
}
