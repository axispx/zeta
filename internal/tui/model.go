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
	"github.com/axispx/zeta/internal/core"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/todo"
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
//
// State is composed into embedded groups: each group owns one concern, and its
// fields are promoted, so call sites read the same (`m.viewport`) while the
// group can be reasoned about — and later given methods — on its own. A field
// owned by a single group never sits at the top level.
type Model struct {
	core.Session

	composerState
	queueState
	transcriptState
	selectionState
	turnState

	width        int
	height       int
	ready        bool
	quitting     bool
	updateOnExit bool          // /update: quit so main updates in the CLI and relaunches
	chrome       styles.Chrome // terminal-derived panels; zero until BackgroundColorMsg
	spinner      spinner.Model // animated while a turn is in flight
	bottom       bottomSlot    // exclusive input-slot panel (perm | ask | plan)
	pendingPlan  string        // plan body produced this turn; offered once on turnDone
	overlay      filterOverlay
	picker       pickerState
	config       configDialog
}

// composerState is the input editor and everything staged in it.
type composerState struct {
	textarea      textarea.Model
	promptHist    promptHistory     // up/down recall of prior user turns
	pendingImages map[int]image.Ref // draft images keyed by stable [Image N] id
	nextImageN    int               // last allocated token number (never renumbered)
}

// queueState is the follow-up queue and its list navigation. editID is the item
// open in the composer, or 0; queueFocus+queueSel drive the panel.
type queueState struct {
	queue       []queuedPrompt // waiting follow-ups (FIFO; oldest at [0])
	editID      int            // queue item id open in composer, or 0
	nextQueueID int            // last allocated follow-up id
	queueFocus  bool           // nav over the follow-ups panel
	queueSel    listSel        // selection while queueFocus
}

// transcriptState is the scrollable conversation surface: the rendered message
// list, the viewport that scrolls it, and the memos that keep repaints cheap.
type transcriptState struct {
	messages      []Message
	viewport      viewport.Model
	contentW      int // wrap width for transcript lines (matches styles.Transcript inset).
	showScrollbar bool
	sessionDiff   lineStats       // memo of sessionDiff(messages); refreshSessionDiff only
	tx            transcriptCache // frozen settled transcript; tail re-renders only
	mainCache     *mainViewCache  // memo of mainView() for transcript + gap; invalidated on transcript change
	paint         streamPaint     // throttled live redraw; gen survives turn boundaries
}

// selectionState is app-level transcript drag selection plus its copy flash.
type selectionState struct {
	sel          transcriptSel // app-level transcript drag selection
	copyFlash    bool          // brief "Copied" in the gap after a successful copy
	copyFlashGen int           // invalidates stale flash timers
}

// turnState is the in-flight work: the agent turn, the compact job's cancel,
// and the id allocator that lets late turn events be dropped.
type turnState struct {
	turn          *turnSession
	nextTurnID    int // last allocated turnSession.id
	compactCancel context.CancelFunc
}

// Options controls how the TUI starts a session.
type Options struct {
	ResumeID string        // non-empty → open that session
	Picker   bool          // true → open session list on start
	Rules    policy.Policy // persisted permission rules (loaded by main)
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
	vp.MouseWheelDelta = 5 // bubbles default is 3
	// SoftWrap: overflow lines become extra display rows (not truncated). Required so
	// YOffset/TotalLineCount and drag selection share one display-line space with
	// wrapContentLines (scrollbar + select both count wrapped rows).
	vp.SoftWrap = true
	// Keep only pgup/pgdn — default keymap also binds j/k/f/space/b/u/d/h/l,
	// which steals those chars from the input and scrolls the transcript.
	vp.KeyMap = viewport.KeyMap{
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
	}

	ws := workspace.Load()
	m := Model{
		transcriptState: transcriptState{viewport: vp, mainCache: &mainViewCache{}},
		composerState:   composerState{textarea: ta},
		Session: core.Session{
			WS:     ws,
			Cfg:    cfg,
			Grants: &permission.Session{},
			Rules:  permission.NewRules(opts.Rules),
			Todos:  todo.NewStore(),
		},
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
	m.promptHist.reset()
	applyTextareaStyles(&m.textarea, nil)
	m.ApplyClient()

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
		m.SeedTodos(nil)
	} else {
		m.Log = sess
		m.SeedTodos(nil)
	}
	if opts.Picker {
		m.openPicker()
	}
	return m, nil
}

// PersistedSessionID returns the current session id if it has been written to disk.
func (m Model) PersistedSessionID() string {
	if m.Log == nil || !m.Log.Persisted() {
		return ""
	}
	return m.Log.ID
}

// UpdateRequested reports that the user ran /update, so main should apply the
// release in the CLI and relaunch zeta.
func (m Model) UpdateRequested() bool {
	return m.updateOnExit
}

func (m *Model) applyPanels(termBg color.Color, dark bool) {
	m.chrome = styles.NewChrome(termBg, dark)
	applyTextareaStyles(&m.textarea, m.chrome.Input)
	// User bubbles bake chrome into the prefix; rebuild on theme change.
	m.invalidate()
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
	return tea.Batch(textarea.Blink, tea.RequestBackgroundColor, checkUpdateCmd())
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
		m.RefreshWorkspace()
		if m.config.active {
			return m, nil
		}
		return m, m.textarea.Focus()

	case tea.BlurMsg:
		// Pointer/focus left the terminal mid-drag → finish like mouse-up.
		cmd := m.finishSelectionDrag()
		m.textarea.Blur()
		return m, cmd

	case copyFlashMsg:
		if msg.gen == m.copyFlashGen {
			m.copyFlash = false
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.refreshTranscript()
		return m, nil

	case sessionTitleMsg:
		m.TitlePending = false
		if msg.err == nil {
			_ = m.ApplyTitle(msg.name)
		}
		return m, nil

	case compactDoneMsg:
		return m, m.handleCompactDone(msg)

	case updateAvailableMsg:
		m.handleUpdateAvailable(msg)
		return m, nil

	case authRetryResultMsg:
		return m, m.handleAuthRetryResult(msg)

	case fileListMsg:
		m.applyFileListMsg(msg)
		return m, nil

	case spinner.TickMsg:
		if !m.busy() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case streamPaintMsg:
		m.handleStreamPaint(msg)
		return m, nil

	default:
		if cmd, ok := m.dispatchTurnMsg(msg); ok {
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if cmd, ok := m.handleBottomClick(msg); ok {
			m.sel.clear()
			return m, cmd
		}
		if cmd, ok := m.handleSelectionMouse(msg); ok {
			return m, cmd
		}

	case tea.MouseMotionMsg:
		if cmd, ok := m.handleSelectionMouse(msg); ok {
			return m, cmd
		}
		if m.handleBottomMotion(msg) {
			return m, nil
		}

	case tea.MouseReleaseMsg:
		if cmd, ok := m.handleSelectionMouse(msg); ok {
			return m, cmd
		}

	case tea.MouseWheelMsg:
		if m.rejectEdgeScroll(msg) { // trackpad momentum past top/bottom
			m.handleSelectionMouse(msg) // still cancel drag
			return m, nil
		}
		m.handleSelectionMouse(msg) // cancel drag; fall through to viewport scroll

	case tea.PasteMsg:
		if m.config.active || m.picker.active || m.inputBlocked() || m.exclusiveJob() {
			break
		}
		// Path attach only; plain text falls through to the textarea.
		if m.handleBracketPaste(msg.Content) {
			return m, nil
		}

	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c":
			return m, m.handleCtrlC()
		case m.config.active:
			cmd, _ := m.updateConfigDialog(msg)
			return m, cmd
		case m.picker.active:
			return m, m.handlePickerKey(msg)
		default:
			if cmd, ok := m.handleBottomKey(msg); ok {
				return m, cmd
			}
			if cmd, ok := m.handleOverlayKey(msg); ok {
				return m, cmd
			}
		}
		// Priority: esc → compact block → ctrl+q → shift+tab →
		// paste → queue nav → prompt history → plain Enter.
		// Overlay nav/commit already handled above via handleOverlayKey.
		switch {
		case msg.String() == "esc" || msg.Code == tea.KeyEscape:
			if m.sel.has() {
				m.sel.clear()
				return m, nil
			}
			// edit → unfocus queue → cancel turn. Never deletes queue items.
			if m.handleQueueEsc() {
				return m, nil
			}
			m.tryInterrupt()
			return m, nil
		case m.exclusiveJob():
			// Block input while compact / self-update runs.
			return m, nil
		case msg.String() == "ctrl+q":
			if m.toggleQueueFocus() {
				return m, nil
			}
		case msg.String() == "shift+tab":
			if m.turn == nil && !m.inputBlocked() && !m.hasQueueState() {
				m.Mode = m.Mode.Next()
				// Mode swaps the developer message and the tool set, so the
				// next request shares no prefix with the last one.
				m.ResetContext()
			}
			return m, nil
		case isPasteKey(msg):
			if !m.inputBlocked() {
				m.handleClipboardPaste()
				return m, nil
			}
		}
		// Queue focus before prompt-history so ↑/↓ move the list, not recall.
		if cmd, ok := m.handleQueueNavKey(msg); ok {
			return m, cmd
		}
		if m.handlePromptHistoryKey(msg) {
			return m, nil
		}
		// Plain Enter only. Never steal shift/alt/ctrl+enter (newlines).
		if msg.Code == tea.KeyEnter && msg.Mod == 0 {
			return m, m.submitInput()
		}
	}

	if m.config.active {
		if cmd, handled := m.updateConfigDialog(msg); handled {
			return m, cmd
		}
		// Unhandled (e.g. already consumed above): keep modal closed to outer chrome.
		return m, nil
	}

	prevH := m.textarea.Height()
	if !m.picker.active && !m.inputBlocked() {
		before := m.textarea.Value()
		m.textarea, taCmd = m.textarea.Update(msg)
		m.notePromptEdit(before)
		if m.textarea.Value() != before {
			m.syncPendingImages()
		}
		m.syncTextareaStyles()
		if m.textarea.Height() != prevH {
			m.refreshTranscript()
		}
	}
	ovCmd := m.syncOverlay()
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, tea.Batch(taCmd, vpCmd, ovCmd)
}

// submit appends the user turn, starts a streaming completion, and refreshes.
// When the transcript is near the context limit, auto-compacts first.
// text/imgs come from parseComposer (inline [Image N] tokens stripped from text).
func (m *Model) submit(text string, imgs []image.Ref) tea.Cmd {
	// Refuse before committing anything: a turn that cannot be sent must stay
	// out of history and off disk, and its text stays in the composer so the
	// user can retry it after /config instead of retyping.
	if m.Client == nil {
		m.noteError("no provider configured, run /config to connect one")
		return nil
	}
	// Exclusive jobs / OAuth recover own the busy slot — callers should queue
	// or no-op first; this is the last line of defense against a competing turn.
	if m.exclusiveJob() || m.AuthRetrying {
		return nil
	}
	if err := m.EnsureFreshClient(context.Background()); err != nil {
		m.noteError(err.Error())
		return nil
	}
	m.AuthRetried = false

	m.commitUserPrompt(text, imgs)
	// Keep an in-progress follow-up edit in the composer (drain of another item).
	if m.editID == 0 {
		m.resetInput()
	}
	m.refreshTranscript()
	// Sending is intentional navigation: always show the new user turn, even if
	// the user had scrolled up to read earlier context (stream paints stay put).
	m.viewport.GotoBottom()

	titlePrompt := text
	if titlePrompt == "" && len(imgs) > 0 {
		titlePrompt = transcriptLabel(imgs[0], 1)
	}
	// Fresh before auto-compact estimate and the turn that follows.
	m.RefreshWorkspace()
	if m.ShouldAutoCompact(m.Client, m.Cfg) {
		return m.runCompact(compactAuto, titlePrompt)
	}
	return m.beginTurn(titlePrompt)
}

// beginTurn starts the agent loop for the current history.
// Callers must RefreshWorkspace first (or have just done so).
func (m *Model) beginTurn(titlePrompt string) tea.Cmd {
	// A new turn: clear the replay gate so a 401 before any output can retry.
	m.Session.BeginTurn()
	if m.Client == nil {
		return nil
	}
	// Defensive: never orphan an in-flight agent loop (e.g. race with submit).
	if m.turn != nil {
		m.finishTurn()
	}
	var cmds []tea.Cmd
	var turnCmd tea.Cmd
	m.nextTurnID++
	m.turn, turnCmd = startTurn(m.nextTurnID, m.Client, &m.Session)
	// Busy gap grows (GapBeforeInput → busyStatusRows); shrink transcript now.
	m.layoutPreservingBottom()
	cmds = append(cmds, turnCmd, m.spinner.Tick)
	if titleCmd := m.ensureTitle(titlePrompt); titleCmd != nil {
		cmds = append(cmds, titleCmd)
	}
	return tea.Batch(cmds...)
}

// ensureTitle requests an AI title once for an untitled session.
func (m *Model) ensureTitle(prompt string) tea.Cmd {
	if m.Client == nil || !m.WantsTitle() {
		return nil
	}
	m.TitlePending = true
	return requestSessionTitle(m.Session, m.Client, prompt)
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

func (m *Model) requestQuit() tea.Cmd {
	m.finishTurn()
	m.cancelCompact()
	m.cancelAuthRetry()
	return m.quit()
}

func (m *Model) quit() tea.Cmd {
	m.quitting = true
	return tea.Quit
}

// persist appends one durable record, surfacing a write failure in the transcript.
func (m *Model) persist(rec session.Record) {
	m.reportSaveErr(m.Session.Persist(rec))
}

// reportSaveErr surfaces a durable-write failure as a transcript error row.
func (m *Model) reportSaveErr(err error) {
	if err == nil {
		return
	}
	m.messages = append(m.messages, Message{
		Role: RoleError,
		Text: "session save failed: " + err.Error(),
	})
}

func loadSession(recs []session.Record) (ui []Message, history []ai.Message) {
	ui = make([]Message, 0, len(recs))
	for _, r := range recs {
		switch r.Role {
		case session.RoleUser:
			ui = append(ui, Message{Role: RoleUser, Text: userDisplayFromSession(r.Text, r.Images)})
		case session.RoleAgent:
			if r.Text != "" {
				ui = append(ui, Message{Role: RoleAgent, Text: r.Text, framePlan: r.FramePlan})
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
// Transcript height accounts for the in-flow gap (status/bottom/idle) + input + footer.
// Filter overlays float over the transcript and do not consume layout rows.
func (m *Model) layout() {
	w := max(m.width, minTermW)

	inputH := max(m.textarea.Height(), inputMinHeight)

	// gap + footer; input chrome is hidden while a bottom panel replaces it.
	chromeH := m.gapHeight() + footerRows
	if !m.inputBlocked() {
		chromeH += inputH + styles.InputChromeV + styles.InputMarginB
	}
	th := max(m.height-chromeH, minTranscriptH)

	// Transcript region (pad + content + pad) may share the row with a scrollbar.
	// styles.Transcript pads ContentInset each side, so viewport width = contentW.
	regionW := w
	if m.showScrollbar {
		regionW -= scrollbarWidth
	}
	if regionW < minInputInnerW+2*styles.ContentInset {
		regionW = minInputInnerW + 2*styles.ContentInset
	}
	contentW := max(regionW-2*styles.ContentInset, minInputInnerW)

	// Input is inset by InputMarginH each side; scrollbar only affects transcript above.
	// lipgloss v2 Width includes padding → textarea = boxW - pad.
	inputInnerW := max(w-styles.InputChromeH-2*styles.InputMarginH, minInputInnerW)

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
	m.invalidateMainView()
}

// refreshTranscript paints immediately and cancels any pending throttled paint.
func (m *Model) refreshTranscript() {
	m.cancelStreamPaint()
	m.repaintTranscript()
}

// repaintTranscript lays out and paints without touching the paint throttle.
//
// Remember if we were at the bottom before painting. Toggling the scrollbar
// changes wrap width, which can make AtBottom lie mid-paint — so only flip the
// bar when needed, then scroll back down if we started at the bottom.
func (m *Model) repaintTranscript() {
	m.invalidateMainView()
	if len(m.messages) == 0 {
		m.showScrollbar = false
		m.layout()
		m.viewport.SetContent("")
		return
	}

	stickBottom := m.viewport.AtBottom()
	m.layout()
	m.setTranscriptContent()
	needBar := m.viewport.TotalLineCount() > m.viewport.Height()
	if needBar != m.showScrollbar {
		m.showScrollbar = needBar
		m.layout()
		m.setTranscriptContent()
	}
	if stickBottom {
		m.viewport.GotoBottom()
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

	// Stack: (transcript + gap [+ floating overlay]) | input? | footer.
	// Filter overlays pin over the bottom of main+gap so the list sits flush on
	// the input without resizing the viewport. Status stays painted in the gap
	// rows (covered only where the list overlaps).
	// layout() already sized the transcript for gapHeight().
	// Empty gap ("") is still one JoinVertical row (idle blank spacer).
	surface := lipgloss.JoinVertical(lipgloss.Left, m.mainView(), m.gapContent())
	if m.filterOverlayOpen() {
		if ov := m.renderOverlay(m.width); ov != "" {
			surface = pinOverlayBottom(surface, ov)
		}
	}
	return m.programView(stackMainChrome(surface, m.renderInput(), m.renderFooter()))
}

// pinOverlayBottom draws overlay over the bottom of main without growing layout.
// main is truncated from the bottom to make room; rows below main stay put.
func pinOverlayBottom(main, overlay string) string {
	if overlay == "" {
		return main
	}
	ovH := lipgloss.Height(overlay)
	if ovH < 1 {
		return main
	}
	mainH := lipgloss.Height(main)
	if mainH < 1 {
		return overlay
	}
	if ovH >= mainH {
		return lipgloss.NewStyle().MaxHeight(mainH).Height(mainH).Render(overlay)
	}
	topH := mainH - ovH
	top := lipgloss.NewStyle().MaxHeight(topH).Height(topH).Render(main)
	return lipgloss.JoinVertical(lipgloss.Left, top, overlay)
}

// renderInput returns the input box, or "" when a bottom panel replaces it.
func (m Model) renderInput() string {
	if m.inputBlocked() {
		return ""
	}
	inputW := max(m.width-2*styles.InputMarginH, minInputInnerW+styles.InputChromeH)
	inputH := max(m.textarea.Height(), inputMinHeight)

	input := m.chrome.InputBox().Width(inputW).Height(inputH + styles.InputPadV).Render(m.textarea.View())
	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH, styles.InputMarginB, styles.InputMarginH).
		Render(input)
}

func (m Model) renderFooter() string {
	footerW := max(m.width-2*styles.InputMarginH, 1)

	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH).
		Render(inputFooter(footerW, m.WS, m.Cfg, m.Mode, m.ContextTokens, m.sessionDiff))
}

// stackMainChrome places the main surface (transcript [+ gap] [+ pinned overlay]),
// optional input, and footer. Gap is folded into main by View before calling.
// Omit input when a bottom panel replaces the composer.
func stackMainChrome(main, input, footer string) string {
	if input == "" {
		return lipgloss.JoinVertical(lipgloss.Left, main, footer)
	}
	return lipgloss.JoinVertical(lipgloss.Left, main, input, footer)
}

func (m Model) programView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	// Enables shift+enter and other modified keys on supporting terminals.
	v.KeyboardEnhancements.ReportEventTypes = true
	// Bubble Tea v2 maps WindowTitle → OSC 2.
	v.WindowTitle = terminalTitle(m.Log)
	return v
}
