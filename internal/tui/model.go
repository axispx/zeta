package tui

import (
	"context"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/compact"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/version"
	"github.com/axispx/zeta/internal/workspace"
)

const (
	inputMinHeight   = 1
	inputMaxHeight   = 8   // visible rows; grows then scrolls
	inputMaxContent  = 500 // total visual lines before input is blocked
	inputPrompt      = "→ "
	inputPromptWidth = 2 // lipgloss width of inputPrompt
	minTermW         = 20
	minTranscriptH   = 3
	minInputInnerW   = 10
)

// Model is the root Bubble Tea model for zeta.
type Model struct {
	cfg      config.Config
	client   *ai.Client
	sess     *session.Session
	viewport viewport.Model
	textarea textarea.Model
	messages []Message
	ws       workspace.Context
	width    int
	height   int
	// contentW is the wrap width for transcript lines (matches styles.Transcript inset).
	contentW      int
	showScrollbar bool
	ready         bool
	quitting      bool
	turn          *turnSession
	history       []ai.Message // durable API transcript (user/assistant/tool); no system/developer
	contextTokens int64        // last response's context footprint (prompt+completion)
	titlePending  bool
	compacting    bool // true while a compact LLM call is in flight (manual or auto)
	compactCancel context.CancelFunc
	mode          prompt.Mode
	grants        *permission.Session // "allow for session"; reset on /clear
	perm          *permissionPrompt
	overlay       filterOverlay
	picker        pickerState
	config        configDialog
	chrome        styles.Chrome   // terminal-derived panels; zero until BackgroundColorMsg
	promptHist    promptHistory   // up/down recall of prior user turns
	spinner       spinner.Model   // animated while a turn is in flight
	tx            transcriptCache // frozen settled transcript; tail re-renders only
	paint         streamPaint     // throttled live redraw; gen survives turn boundaries
}

// Options controls how the TUI starts a session.
type Options struct {
	ResumeID string // non-empty → open that session
	Picker   bool   // true → open session list on start
}

// New creates the initial TUI model.
func New(cfg config.Config, opts Options) (Model, error) {
	ta := textarea.New()
	ta.Placeholder = ""
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.DynamicHeight = true
	ta.MinHeight = inputMinHeight
	ta.MaxHeight = inputMaxHeight
	// Without MaxContentHeight, MaxHeight also caps content (no scroll). Set it
	// so MaxHeight is only the visible viewport and overflow scrolls.
	ta.MaxContentHeight = inputMaxContent
	ta.SetHeight(inputMinHeight)
	// Prompt only on the first visual line; continuation lines stay blank but
	// keep the same gutter width so text stays aligned.
	ta.SetPromptFunc(inputPromptWidth, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return inputPrompt
		}
		return ""
	})
	// shift+enter needs Kitty keyboard protocol (or CSI-u / modifyOtherKeys).
	// ctrl+j is the universal fallback: LF (0x0A) is distinct from Enter's CR (0x0D).
	// alt+enter is common on macOS when Option-as-Meta is on.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j", "alt+enter"),
		key.WithHelp("shift+enter", "newline"),
	)

	ta.Focus()

	vp := viewport.New()
	vp.MouseWheelEnabled = true
	// Keep only pgup/pgdn — default keymap also binds j/k/f/space/b/u/d/h/l,
	// which steals those chars from the input and scrolls the transcript.
	vp.KeyMap = viewport.KeyMap{
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
	}

	ws := workspace.Load()
	m := Model{
		cfg:      cfg,
		viewport: vp,
		textarea: ta,
		ws:       ws,
		spinner:  spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		grants:   &permission.Session{},
	}
	m.promptHist.reset()
	applyTextareaStyles(&m.textarea, nil)
	m.applyClient()

	if opts.ResumeID != "" {
		sess, recs, err := session.OpenID(ws.Abs, opts.ResumeID)
		if err != nil {
			return Model{}, err
		}
		m.applySession(sess, recs, nil)
		return m, nil
	}

	if sess, err := session.New(ws.Abs); err != nil {
		m.messages = []Message{{Role: RoleError, Text: "session: " + err.Error()}}
	} else {
		m.sess = sess
	}
	if opts.Picker {
		m.openPicker()
	}
	return m, nil
}

// PersistedSessionID returns the current session id if it has been written to disk.
func (m Model) PersistedSessionID() string {
	if m.sess == nil || !m.sess.Persisted() {
		return ""
	}
	return m.sess.ID
}

func (m *Model) applyPanels(termBg color.Color, dark bool) {
	m.chrome = styles.NewChrome(termBg, dark)
	applyTextareaStyles(&m.textarea, m.chrome.Input)
	// User bubbles bake chrome into the prefix; rebuild on theme change.
	m.tx.invalidate()
}

// applyTextareaStyles sets textarea chrome; bg nil skips panel fill (pre-BackgroundColorMsg).
// Empty input dims the focused prompt arrow.
func applyTextareaStyles(ta *textarea.Model, bg color.Color) {
	ts := textarea.DefaultStyles(true)
	base := lipgloss.NewStyle()
	prompt := styles.Prompt
	ph := styles.Placeholder
	if bg != nil {
		base = base.Background(bg)
		prompt = prompt.Background(bg)
		ph = ph.Background(bg)
	}
	focusedPrompt := prompt
	if ta.Value() == "" {
		focusedPrompt = prompt.Faint(true)
	}
	ts.Focused.Base = base
	ts.Focused.Text = base
	ts.Focused.CursorLine = base
	ts.Focused.Placeholder = ph
	ts.Focused.Prompt = focusedPrompt
	ts.Blurred.Base = base
	ts.Blurred.Text = base
	ts.Blurred.CursorLine = base
	ts.Blurred.Placeholder = ph
	ts.Blurred.Prompt = prompt.Faint(true)
	ts.Cursor.Color = styles.White
	ts.Cursor.Blink = false
	ta.SetStyles(ts)
}

func (m *Model) syncTextareaStyles() {
	applyTextareaStyles(&m.textarea, m.chrome.Input)
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, tea.RequestBackgroundColor)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		taCmd tea.Cmd
		vpCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.applyPanels(msg, msg.IsDark())
		m.refreshTranscript()
		return m, nil

	case tea.FocusMsg:
		if m.config.active {
			return m, nil
		}
		return m, m.textarea.Focus()

	case tea.BlurMsg:
		m.textarea.Blur()
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.refreshTranscript()
		return m, nil

	case sessionTitleMsg:
		m.titlePending = false
		if msg.err == nil && msg.name != "" && m.sess != nil {
			_ = m.sess.SetName(msg.name)
		}
		return m, nil

	case compactDoneMsg:
		return m, m.handleCompactDone(msg)

	case spinner.TickMsg:
		if !m.busy() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case turnDeltaMsg:
		return m, m.handleTurnDelta(msg)

	case turnReasoningMsg:
		return m, m.handleTurnReasoning(msg)

	case streamPaintMsg:
		m.handleStreamPaint(msg)
		return m, nil

	case turnAssistantMsg:
		return m, m.handleTurnAssistant(msg)

	case turnToolStartMsg:
		return m, m.handleTurnToolStart(msg)

	case turnToolOutMsg:
		return m, m.handleTurnToolOut(msg)

	case turnToolMsg:
		return m, m.handleTurnTool(msg)

	case turnDoneMsg:
		m.finishTurn()
		m.refreshTranscript()
		return m, nil

	case turnErrMsg:
		m.handleTurnErr(msg.err)
		return m, nil

	case tea.MouseClickMsg:
		if m.handlePermissionClick(msg) {
			return m, nil
		}

	case tea.MouseMotionMsg:
		if m.handlePermissionMotion(msg) {
			return m, nil
		}

	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c":
			if m.tryInterrupt() {
				return m, nil
			}
			return m, m.requestQuit()
		case m.config.active:
			cmd, _ := m.config.Update(msg)
			return m, cmd
		case m.picker.active:
			return m, m.handlePickerKey(msg)
		case m.handlePermissionKey(msg):
			return m, nil
		case m.overlay.mode == overlayModels:
			if cmd, ok := m.handleOverlayKey(msg); ok {
				return m, cmd
			}
		case msg.String() == "esc":
			m.tryInterrupt()
			return m, nil
		case m.compacting:
			// Block input while compaction runs (result arrives via compactDoneMsg).
			return m, nil
		case m.consumeCommandOverlayKey(msg):
			return m, nil
		case msg.String() == "shift+tab":
			if m.turn == nil {
				m.mode = m.mode.Next()
			}
			return m, nil
		case m.handlePromptHistoryKey(msg):
			return m, nil
		// Plain Enter only. Never steal shift/alt/ctrl+enter — those are newlines.
		case msg.Code == tea.KeyEnter && msg.Mod == 0:
			return m, m.submitInput()
		}
	}

	if m.config.active {
		if cmd, handled := m.config.Update(msg); handled {
			return m, cmd
		}
		// Unhandled (e.g. already consumed above): keep modal closed to outer chrome.
		return m, nil
	}

	prevH := m.textarea.Height()
	if !m.picker.active && m.perm == nil {
		before := m.textarea.Value()
		m.textarea, taCmd = m.textarea.Update(msg)
		m.notePromptEdit(before)
		m.syncTextareaStyles()
		if m.textarea.Height() != prevH {
			m.refreshTranscript()
		}
	}
	m.syncOverlay()
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, tea.Batch(taCmd, vpCmd)
}

// submit appends the user turn, starts a streaming completion, and refreshes.
// When the transcript is near the context limit, auto-compacts first.
func (m *Model) submit(text string) tea.Cmd {
	user := Message{Role: RoleUser, Text: text}
	m.messages = append(m.messages, user)
	m.history = append(m.history, ai.Message{Role: ai.RoleUser, Text: text})
	m.persist(session.Record{Role: session.RoleUser, Text: text})
	m.resetInput()
	m.refreshTranscript()

	if m.client == nil {
		errMsg := Message{Role: RoleError, Text: "no provider configured — set up ~/.zeta/config.json"}
		m.messages = append(m.messages, errMsg)
		m.persist(session.Record{Role: session.RoleError, Text: errMsg.Text})
		m.refreshTranscript()
		return nil
	}
	if err := m.ensureFreshClient(); err != nil {
		errMsg := Message{Role: RoleError, Text: "oauth refresh: " + err.Error()}
		m.messages = append(m.messages, errMsg)
		m.persist(session.Record{Role: session.RoleError, Text: errMsg.Text})
		m.refreshTranscript()
		return nil
	}

	if m.shouldAutoCompact() {
		return m.runCompact(compactAuto, text)
	}
	return m.beginTurn(text)
}

// beginTurn starts the agent loop for the current history.
func (m *Model) beginTurn(titlePrompt string) tea.Cmd {
	if m.client == nil {
		return nil
	}
	var cmds []tea.Cmd
	var turnCmd tea.Cmd
	m.turn, turnCmd = startTurn(m.client, m.ws, m.mode, m.history, m.grants)
	// Busy gap grows (GapBeforeInput → busyStatusRows); shrink transcript now.
	m.layoutPreservingBottom()
	cmds = append(cmds, turnCmd, m.spinner.Tick)
	if titleCmd := m.ensureTitle(titlePrompt); titleCmd != nil {
		cmds = append(cmds, titleCmd)
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleTurnDelta(msg turnDeltaMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	m.turn.beginStreaming() // clears thinking; pending/next paint drops the chrome
	n := len(m.messages)
	if n > 0 && m.messages[n-1].Role == RoleAgent {
		m.messages[n-1].Text += msg.text
	} else {
		m.messages = append(m.messages, Message{Role: RoleAgent, Text: msg.text})
	}
	// Ingest every token; paint at most every streamPaintEvery.
	return tea.Batch(m.requestStreamPaint(), waitTurn(m.turn))
}

// handleTurnReasoning appends pre-answer reasoning for the live tail.
// Outside thinkingPhase tokens are ignored; the stream is still drained.
func (m *Model) handleTurnReasoning(msg turnReasoningMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	if !m.turn.acceptReasoning(msg.text) {
		return waitTurn(m.turn)
	}
	return tea.Batch(m.requestStreamPaint(), waitTurn(m.turn))
}

func (m *Model) handleTurnAssistant(msg turnAssistantMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	// Segment done — flush buffered answer / clear thinking immediately.
	if m.turn.endStreaming() {
		m.refreshTranscript()
	}
	m.history = append(m.history, msg.message)
	if n := msg.usage.ContextTokens(); n > 0 {
		m.contextTokens = n
	}
	m.persist(recordFromAPI(msg.message))
	return waitTurn(m.turn)
}

func (m *Model) handleTurnToolStart(msg turnToolStartMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	m.turn.endStreaming()
	label := msg.label
	if label == "" {
		label = msg.name
	}
	m.messages = append(m.messages, newToolMessage(label, msg.name))
	m.turn.activeTool = len(m.messages) - 1
	if detail := strings.TrimSpace(msg.detail); detail != "" {
		m.messages[m.turn.activeTool].Out = detail
	}
	m.refreshTranscript()

	// Agent only waits when Gate is true. Match NeedsDecision — do not send
	// a Decision the agent isn't awaiting.
	if !permission.NeedsDecision(m.grants, msg.name) {
		return waitTurn(m.turn)
	}
	m.perm = &permissionPrompt{
		label: label,
		name:  msg.name,
		path:  msg.path,
	}
	if m.ready {
		m.layoutPreservingBottom()
	}
	return waitTurn(m.turn)
}

func (m *Model) handleTurnToolOut(msg turnToolOutMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	if i := m.turn.activeTool; i >= 0 && i < len(m.messages) && m.messages[i].Tool == msg.name {
		m.messages[i].Out = msg.text
		return tea.Batch(m.requestStreamPaint(), waitTurn(m.turn))
	}
	return waitTurn(m.turn)
}

func (m *Model) handleTurnTool(msg turnToolMsg) tea.Cmd {
	if m.turn == nil {
		return nil
	}
	m.history = append(m.history, msg.message)
	if i := m.turn.activeTool; i >= 0 && i < len(m.messages) && m.messages[i].Tool == msg.name {
		if msg.denied {
			m.messages[i].Status = ToolDenied
		} else {
			m.messages[i].Status = ToolOK
			if toolHasOut(m.messages[i].Tool) {
				m.messages[i].Out = msg.message.Text
			}
		}
	}
	m.turn.activeTool = -1
	m.persist(toolRecord(msg.message, msg.label, msg.name, msg.denied))
	m.refreshTranscript()
	return waitTurn(m.turn)
}

// ensureTitle requests an AI title once for an untitled session.
func (m *Model) ensureTitle(prompt string) tea.Cmd {
	if m.client == nil || m.sess == nil || m.titlePending || m.sess.Name != "" {
		return nil
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil
	}
	m.titlePending = true
	return requestSessionTitle(m.client, prompt)
}

func firstUserPrompt(msgs []Message) string {
	for _, msg := range msgs {
		if msg.Role == RoleUser {
			if t := strings.TrimSpace(msg.Text); t != "" {
				return t
			}
		}
	}
	return ""
}

func (m *Model) finishTurn() {
	if m.turn == nil {
		return
	}
	m.cancelStreamPaint() // invalidate pending ticks (gen is on Model)
	// Deny any open ask so the agent unblocks on the same path as user deny.
	m.abandonPermission()
	// Cancel mid-tool: close out the open row — late KindTool is dropped.
	if i := m.turn.activeTool; i >= 0 && i < len(m.messages) && m.messages[i].Status == ToolRunning {
		m.messages[i].Status = ToolDenied
	}
	m.turn.cancel()
	m.turn = nil
	m.history = compact.TrimIncomplete(m.history)
}

func (m *Model) requestQuit() tea.Cmd {
	m.finishTurn()
	m.cancelCompact()
	return m.quit()
}

func (m *Model) quit() tea.Cmd {
	m.quitting = true
	return tea.Quit
}

func (m *Model) handleTurnErr(err error) {
	if m.turn == nil {
		return
	}
	m.finishTurn()
	errMsg := Message{Role: RoleError, Text: err.Error()}
	m.messages = append(m.messages, errMsg)
	m.persist(session.Record{Role: session.RoleError, Text: errMsg.Text})
	m.refreshTranscript()
}

func (m *Model) persist(rec session.Record) {
	if m.sess == nil {
		return
	}
	if err := m.sess.Append(rec); err != nil {
		m.messages = append(m.messages, Message{
			Role: RoleError,
			Text: "session save failed: " + err.Error(),
		})
	}
}

func recordFromAPI(m ai.Message) session.Record {
	switch m.Role {
	case ai.RoleUser:
		return session.Record{Role: session.RoleUser, Text: m.Text}
	case ai.RoleAssistant:
		rec := session.Record{Role: session.RoleAgent, Text: m.Text}
		for _, tc := range m.ToolCalls {
			rec.ToolCalls = append(rec.ToolCalls, session.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
		return rec
	case ai.RoleTool:
		return session.Record{
			Role:       session.RoleTool,
			Text:       m.Text,
			ToolCallID: m.ToolCallID,
		}
	default:
		return session.Record{Role: session.RoleError, Text: m.Text}
	}
}

func toolRecord(m ai.Message, label, name string, denied bool) session.Record {
	rec := recordFromAPI(m)
	rec.Label = label
	rec.Tool = name
	rec.Denied = denied
	return rec
}

func loadSession(recs []session.Record) (ui []Message, history []ai.Message) {
	ui = make([]Message, 0, len(recs))
	for _, r := range recs {
		switch r.Role {
		case session.RoleUser:
			ui = append(ui, Message{Role: RoleUser, Text: r.Text})
		case session.RoleAgent:
			if r.Text != "" {
				ui = append(ui, Message{Role: RoleAgent, Text: r.Text})
			}
		case session.RoleTool:
			label := r.Label
			if label == "" {
				label = "tool"
			}
			uiMsg := newToolMessage(label, r.Tool)
			if r.Denied {
				uiMsg.Status = ToolDenied
			} else {
				uiMsg.Status = ToolOK
			}
			if toolHasOut(r.Tool) && uiMsg.Status == ToolOK {
				uiMsg.Out = r.Text
			}
			ui = append(ui, uiMsg)
		case session.RoleCompact:
			// Full JSONL is kept for the UI; API history is rebuilt below.
			ui = append(ui, Message{Role: RoleSystem, Text: compactDividerText})
		case session.RoleError:
			ui = append(ui, Message{Role: RoleError, Text: r.Text})
		}
	}
	return ui, compact.RebuildAPIHistory(recs)
}

// layout sizes chrome regions. m.showScrollbar reserves one column for the transcript scrollbar.
// Transcript height accounts for the real gap (busy status / overlay / reserved blank).
func (m *Model) layout() {
	w := m.width
	if w < minTermW {
		w = minTermW
	}

	inputH := m.textarea.Height()
	if inputH < inputMinHeight {
		inputH = inputMinHeight
	}

	// gap + footer; input chrome is hidden while a permission prompt is open.
	chromeH := m.gapHeight() + 1 // footer
	if m.perm == nil {
		chromeH += inputH + styles.InputChromeV + styles.InputMarginB
	}
	th := m.height - chromeH
	if th < minTranscriptH {
		th = minTranscriptH
	}

	// Transcript region (pad + content + pad) may share the row with a scrollbar.
	// styles.Transcript pads ContentInset each side, so viewport width = contentW.
	regionW := w
	if m.showScrollbar {
		regionW -= scrollbarWidth
	}
	if regionW < minInputInnerW+2*styles.ContentInset {
		regionW = minInputInnerW + 2*styles.ContentInset
	}
	contentW := regionW - 2*styles.ContentInset
	if contentW < minInputInnerW {
		contentW = minInputInnerW
	}

	// Input is inset by InputMarginH each side; scrollbar only affects transcript above.
	// lipgloss v2 Width includes padding → textarea = boxW - pad.
	inputInnerW := w - styles.InputChromeH - 2*styles.InputMarginH
	if inputInnerW < minInputInnerW {
		inputInnerW = minInputInnerW
	}

	m.contentW = contentW
	m.viewport.SetWidth(contentW)
	m.viewport.SetHeight(th)
	m.textarea.SetWidth(inputInnerW)
}

// layoutPreservingBottom re-runs layout and keeps stick-to-bottom scroll when
// chrome height changes (busy gap, overlay) without rewriting transcript content.
func (m *Model) layoutPreservingBottom() {
	atBottom := m.viewport.AtBottom()
	m.layout()
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// refreshTranscript paints immediately and cancels any pending throttled paint.
func (m *Model) refreshTranscript() {
	m.cancelStreamPaint()
	m.repaintTranscript()
}

// repaintTranscript lays out and paints without touching the paint throttle.
func (m *Model) repaintTranscript() {
	m.showScrollbar = false
	m.layout()
	if len(m.messages) == 0 {
		m.viewport.SetContent("")
		return
	}
	m.setTranscriptContent()
	if m.viewport.TotalLineCount() > m.viewport.Height() {
		m.showScrollbar = true
		m.layout()
		m.setTranscriptContent()
	}
}

func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	if !m.ready {
		return m.programView(styles.SystemMsg.Render("loading…"))
	}
	if m.picker.active {
		w, h := m.width, m.height
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		return m.programView(m.renderPicker(w, h))
	}
	if m.config.active {
		w, h := m.width, m.height
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		return m.programView(m.config.View(m.chrome, w, w, h))
	}

	// gap is one layout slot: permission panel, busy status, command overlay, or blank.
	// layout() already sized the transcript for gapHeight().
	gap := m.gapContent()
	input := m.renderInput()
	footer := m.renderFooter()
	return m.programView(stackMainChrome(m.mainView(), gap, input, footer))
}

// renderInput returns the input box, or "" when a permission prompt replaces it.
func (m Model) renderInput() string {
	if m.perm != nil {
		return ""
	}
	inputW := m.width - 2*styles.InputMarginH
	if inputW < minInputInnerW+styles.InputChromeH {
		inputW = minInputInnerW + styles.InputChromeH
	}
	inputH := m.textarea.Height()
	if inputH < inputMinHeight {
		inputH = inputMinHeight
	}
	input := m.chrome.InputBox().Width(inputW).Height(inputH + styles.InputPadV).Render(m.textarea.View())
	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH, styles.InputMarginB, styles.InputMarginH).
		Render(input)
}

func (m Model) renderFooter() string {
	footerW := m.width - 2*styles.InputMarginH
	if footerW < 1 {
		footerW = 1
	}
	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH).
		Render(inputFooter(footerW, m.ws, m.cfg, m.mode, m.contextTokens))
}

// stackMainChrome places transcript, gap row (status/overlay/blank), input, and footer.
// Omit input when empty (permission prompt replaces it).
func stackMainChrome(main, gap, input, footer string) string {
	if input == "" {
		return lipgloss.JoinVertical(lipgloss.Left, main, gap, footer)
	}
	return lipgloss.JoinVertical(lipgloss.Left, main, gap, input, footer)
}

func (m Model) programView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	// Enables shift+enter and other modified keys on supporting terminals.
	v.KeyboardEnhancements.ReportEventTypes = true
	// Bubble Tea v2 maps WindowTitle → OSC 2.
	v.WindowTitle = terminalTitle(m.sess)
	return v
}

func (m Model) mainView() string {
	w := m.viewport.Width()
	h := m.viewport.Height()
	if w <= 0 || h <= 0 {
		return ""
	}

	var inner string
	if len(m.messages) == 0 {
		banner := styles.Banner.Render(strings.TrimSpace(styles.BannerArt))
		ver := styles.Placeholder.Render("v" + version.Version)
		hero := lipgloss.JoinVertical(lipgloss.Center, banner, "", ver)
		inner = lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, hero)
	} else {
		inner = m.viewport.View()
	}

	body := styles.Transcript.Render(inner)
	if !m.showScrollbar {
		return body
	}
	bar := renderScrollbar(h, m.viewport.TotalLineCount(), m.viewport.YOffset())
	return lipgloss.JoinHorizontal(lipgloss.Top, body, bar)
}
