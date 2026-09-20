// Package tui is the terminal UI for zeta.
//
// Model is the root Bubble Tea model: it routes messages, composes the frame,
// and owns the interaction surfaces around the transcript — the composer, the
// follow-up queue, the input-row panels that gate tool calls (permission, ask,
// plan), and the picker, config, and overlay views. The session and every
// decision a turn needs live in internal/core; this package renders that state
// and reports the user's answer back.
package tui

import (
	"image/color"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/core"
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

// Model is the root Bubble Tea model for zeta
type Model struct {
	session core.Session

	// Screen regions, top to bottom.
	transcript transcript
	queue      queue
	panel      panel
	composer   composer

	// Live interaction state.
	turn        turn
	spinner     spinner.Model
	pendingPlan string
	selection   selection
	overlay     filterOverlay
	picker      pickerState
	config      configDialog

	// Terminal environment and process lifecycle.
	term term
	exit exit
}

// term is what the terminal told us about itself: geometry from
// tea.WindowSizeMsg and chrome from tea.BackgroundColorMsg. Those messages are
// the only writers, so every layout and render sees the same ambient snapshot.
type term struct {
	width  int
	height int
	ready  bool          // first WindowSizeMsg seen; layout is meaningless before
	chrome styles.Chrome // panels/ink derived from the terminal background
}

// exit is why the process is winding down, if it is. quitting gates rendering
// and message handling; updateOnExit tells main to apply the release and
// relaunch instead of just leaving.
type exit struct {
	quitting     bool
	updateOnExit bool // /update: quit so main updates in the CLI and relaunches
}

// Options controls how the TUI starts a session.
type Options struct {
	ResumeID string        // non-empty → open that session
	Picker   bool          // true → open session list on start
	Rules    policy.Policy // persisted permission rules (loaded by main)
}

// newTranscriptViewport builds the transcript's viewport. New and the test
// model both come through here: a test viewport configured differently would
// silently exercise a viewport the program never shows.
func newTranscriptViewport() viewport.Model {
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
	return vp
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

	vp := newTranscriptViewport()

	ws := workspace.Load()
	m := Model{
		transcript: transcript{viewport: vp, mainCache: &mainViewCache{}},
		composer:   composer{textarea: ta},
		session: core.Session{
			WS:     ws,
			Cfg:    cfg,
			Grants: &permission.Session{},
			Rules:  permission.NewRules(opts.Rules),
			Todos:  todo.NewStore(),
		},
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
	m.composer.promptHist.reset()
	applyTextareaStyles(&m.composer.textarea, nil)
	m.session.ApplyClient()

	if opts.ResumeID != "" {
		sess, recs, err := session.OpenID(ws.Abs, opts.ResumeID)
		if err != nil {
			return Model{}, err
		}
		m.applySession(sess, recs, nil)
		return m, nil
	}

	if sess, err := session.New(ws.Abs); err != nil {
		m.transcript.messages = []Message{{Role: RoleError, Text: "session: " + err.Error()}}
		m.session.SeedTodos(nil)
	} else {
		m.session.Log = sess
		m.session.SeedTodos(nil)
	}
	if opts.Picker {
		m.openPicker()
	}
	return m, nil
}

func (m *Model) applyPanels(termBg color.Color, dark bool) {
	m.term.chrome = styles.NewChrome(termBg, dark)
	applyTextareaStyles(&m.composer.textarea, m.term.chrome.Input)
	// User bubbles bake chrome into the prefix; rebuild on theme change.
	m.transcript.invalidate()
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
		m.session.RefreshWorkspace()
		if m.config.active {
			return m, nil
		}
		return m, m.composer.textarea.Focus()

	case tea.BlurMsg:
		// Pointer/focus left the terminal mid-drag → finish like mouse-up.
		cmd := m.finishSelectionDrag()
		m.composer.textarea.Blur()
		return m, cmd

	case copyFlashMsg:
		if msg.gen == m.selection.copyFlashGen {
			m.selection.copyFlash = false
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.term.width = msg.Width
		m.term.height = msg.Height
		m.term.ready = true
		m.refreshTranscript()
		return m, nil

	case sessionTitleMsg:
		m.session.TitlePending = false
		if msg.err == nil {
			_ = m.session.ApplyTitle(msg.name)
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
		if cmd, ok := m.handlePanelClick(msg); ok {
			m.selection.sel.clear()
			return m, cmd
		}
		if cmd, ok := m.handleSelectionMouse(msg); ok {
			return m, cmd
		}

	case tea.MouseMotionMsg:
		if cmd, ok := m.handleSelectionMouse(msg); ok {
			return m, cmd
		}
		if m.handlePanelMotion(msg) {
			return m, nil
		}

	case tea.MouseReleaseMsg:
		if cmd, ok := m.handleSelectionMouse(msg); ok {
			return m, cmd
		}

	case tea.MouseWheelMsg:
		if m.transcript.rejectEdgeScroll(msg) { // trackpad momentum past top/bottom
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
			if cmd, ok := m.handlePanelKey(msg); ok {
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
			if m.selection.sel.has() {
				m.selection.sel.clear()
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
			if m.turn.current == nil && !m.inputBlocked() && !m.queue.hasState() {
				m.session.Mode = m.session.Mode.Next()
				// Mode swaps the developer message and the tool set, so the
				// next request shares no prefix with the last one.
				m.session.ResetContext()
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

	prevH := m.composer.textarea.Height()
	if !m.picker.active && !m.inputBlocked() {
		before := m.composer.textarea.Value()
		m.composer.textarea, taCmd = m.composer.textarea.Update(msg)
		m.notePromptEdit(before)
		if m.composer.textarea.Value() != before {
			m.syncPendingImages()
		}
		m.syncTextareaStyles()
		if m.composer.textarea.Height() != prevH {
			m.refreshTranscript()
		}
	}
	ovCmd := m.syncOverlay()
	m.transcript.viewport, vpCmd = m.transcript.viewport.Update(msg)
	return m, tea.Batch(taCmd, vpCmd, ovCmd)
}
