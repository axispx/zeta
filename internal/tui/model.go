package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/config"
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
	stream        *streamSession
	titlePending  bool
	mode          prompt.Mode
	overlay       filterOverlay
	picker        pickerState
}

// New creates the initial TUI model.
func New(cfg config.Config) Model {
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

	ts := textarea.DefaultStyles(true)
	inputBase := lipgloss.NewStyle().Foreground(styles.Fg).Background(styles.BgInput)
	ts.Focused.Base = inputBase
	ts.Focused.CursorLine = lipgloss.NewStyle()
	ts.Focused.Placeholder = styles.Placeholder.Background(styles.BgInput)
	ts.Focused.Prompt = styles.Prompt.Background(styles.BgInput)
	ts.Blurred.Base = inputBase
	ts.Blurred.CursorLine = lipgloss.NewStyle()
	ts.Blurred.Placeholder = styles.Placeholder.Background(styles.BgInput)
	ts.Blurred.Prompt = styles.Prompt.Foreground(styles.Dim).Background(styles.BgInput)
	ts.Cursor.Color = styles.Fg
	ts.Cursor.Blink = false
	ta.SetStyles(ts)
	ta.Focus()

	vp := viewport.New()
	vp.MouseWheelEnabled = true

	ws := workspace.Load()
	m := Model{
		cfg:      cfg,
		viewport: vp,
		textarea: ta,
		ws:       ws,
	}
	m.applyClient()
	if sess, err := session.New(ws.Abs); err != nil {
		m.messages = []Message{{Role: RoleError, Text: "session: " + err.Error()}}
	} else {
		m.sess = sess
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		taCmd tea.Cmd
		vpCmd tea.Cmd
	)

	switch msg := msg.(type) {
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

	case streamDeltaMsg:
		if m.stream == nil {
			return m, nil
		}
		n := len(m.messages)
		if n > 0 && m.messages[n-1].Role == RoleAgent {
			m.messages[n-1].Text += msg.text
		} else {
			m.messages = append(m.messages, Message{Role: RoleAgent, Text: msg.text})
		}
		m.refreshTranscript()
		return m, waitStreamEvent(m.stream.ch)

	case streamDoneMsg:
		m.finishStream()
		m.refreshTranscript()
		return m, nil

	case streamErrMsg:
		m.handleStreamErr(msg.err)
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c":
			m.finishStream()
			m.quitting = true
			return m, tea.Quit
		case m.picker.active:
			return m, m.handlePickerKey(msg)
		case m.overlay.mode == overlayModels:
			if cmd, ok := m.handleOverlayKey(msg); ok {
				return m, cmd
			}
		case msg.String() == "esc":
			if m.overlay.mode == overlayCommands && m.overlay.showing() {
				m.dismissOverlay()
				return m, nil
			}
			if m.stream != nil {
				m.finishStream()
				m.refreshTranscript()
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		case m.consumeCommandOverlayKey(msg):
			return m, nil
		case msg.String() == "shift+tab":
			if m.stream == nil {
				m.mode = m.mode.Next()
			}
			return m, nil
		// Plain Enter only. Never steal shift/alt/ctrl+enter — those are newlines.
		case msg.Code == tea.KeyEnter && msg.Mod == 0:
			return m, m.submitInput()
		}
	}

	prevH := m.textarea.Height()
	if !m.picker.active {
		m.textarea, taCmd = m.textarea.Update(msg)
		if m.textarea.Height() != prevH {
			m.refreshTranscript()
		}
	}
	m.syncOverlay()
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, tea.Batch(taCmd, vpCmd)
}

// submit appends the user turn, starts a streaming completion, and refreshes.
func (m *Model) submit(text string) tea.Cmd {
	user := Message{Role: RoleUser, Text: text}
	m.messages = append(m.messages, user)
	m.persist(user)
	m.resetInput()
	m.refreshTranscript()

	if m.client == nil {
		errMsg := Message{Role: RoleError, Text: "no provider configured — set up ~/.zeta/config.json"}
		m.messages = append(m.messages, errMsg)
		m.persist(errMsg)
		m.refreshTranscript()
		return nil
	}

	var cmds []tea.Cmd
	var streamCmd tea.Cmd
	m.stream, streamCmd = startStream(m.client, m.ws, m.mode, m.messages)
	cmds = append(cmds, streamCmd)
	if titleCmd := m.ensureTitle(text); titleCmd != nil {
		cmds = append(cmds, titleCmd)
	}
	return tea.Batch(cmds...)
}

// ensureTitle requests an AI title once for an untitled session.
func (m *Model) ensureTitle(prompt string) tea.Cmd {
	if m.client == nil || m.sess == nil || m.titlePending {
		return nil
	}
	if name, err := m.sess.IndexedName(); err != nil || name != "" {
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

func (m *Model) finishStream() {
	if m.stream == nil {
		return
	}
	m.persistLastAgent()
	m.clearStream()
}

func (m *Model) handleStreamErr(err error) {
	m.finishStream()
	errMsg := Message{Role: RoleError, Text: err.Error()}
	m.messages = append(m.messages, errMsg)
	m.persist(errMsg)
	m.refreshTranscript()
}

func (m *Model) clearStream() {
	if m.stream != nil {
		m.stream.cancel()
		m.stream = nil
	}
}

func (m *Model) persistLastAgent() {
	n := len(m.messages)
	if n == 0 || m.messages[n-1].Role != RoleAgent {
		return
	}
	text := m.messages[n-1].Text
	if text == "" {
		return
	}
	m.persist(Message{Role: RoleAgent, Text: text})
}

func (m *Model) persist(msg Message) {
	if m.sess == nil {
		return
	}
	role, ok := toSessionRole(msg.Role)
	if !ok {
		return
	}
	if err := m.sess.Append(role, msg.Text); err != nil {
		m.messages = append(m.messages, Message{
			Role: RoleError,
			Text: "session save failed: " + err.Error(),
		})
	}
}

func toSessionRole(r Role) (string, bool) {
	switch r {
	case RoleUser:
		return session.RoleUser, true
	case RoleAgent:
		return session.RoleAgent, true
	case RoleError:
		return session.RoleError, true
	default:
		return "", false
	}
}

func messagesFromRecords(recs []session.Record) []Message {
	out := make([]Message, 0, len(recs))
	for _, r := range recs {
		role, ok := fromSessionRole(r.Role)
		if !ok {
			continue
		}
		out = append(out, Message{Role: role, Text: r.Text})
	}
	return out
}

func fromSessionRole(r string) (Role, bool) {
	switch r {
	case session.RoleUser:
		return RoleUser, true
	case session.RoleAgent:
		return RoleAgent, true
	case session.RoleError:
		return RoleError, true
	default:
		return 0, false
	}
}

// layout sizes chrome regions. m.showScrollbar reserves one column for the transcript scrollbar.
func (m *Model) layout() {
	w := m.width
	if w < minTermW {
		w = minTermW
	}

	inputH := m.textarea.Height()
	if inputH < inputMinHeight {
		inputH = inputMinHeight
	}

	// gap + input body + margin below + footer (cwd/model)
	chromeH := styles.GapBeforeInput + inputH + styles.InputChromeV + styles.InputMarginB + 1
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

func (m *Model) refreshTranscript() {
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

func (m *Model) setTranscriptContent() {
	var b strings.Builder
	last := len(m.messages) - 1
	for i := range m.messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		top := 0
		if i == 0 {
			top = 1
		}
		msg := &m.messages[i]
		// Live stream stays plain (avoids half-open fence flicker); Model owns that policy.
		if m.stream != nil && i == last && msg.Role == RoleAgent {
			body := plainAgent(msg.Text, m.contentW)
			if top > 0 {
				body = lipgloss.NewStyle().MarginTop(top).Render(body)
			}
			b.WriteString(body)
			continue
		}
		b.WriteString(msg.render(m.contentW, top))
	}
	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
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

	main := m.mainView()
	// Inset by InputMarginH each side (matches transcript ContentInset).
	inputW := m.width - 2*styles.InputMarginH
	if inputW < minInputInnerW+styles.InputChromeH {
		inputW = minInputInnerW + styles.InputChromeH
	}
	inputH := m.textarea.Height()
	if inputH < inputMinHeight {
		inputH = inputMinHeight
	}
	input := styles.InputBox.Width(inputW).Height(inputH + styles.InputPadV).Render(m.textarea.View())
	input = lipgloss.NewStyle().
		Margin(0, styles.InputMarginH, styles.InputMarginB, styles.InputMarginH).
		Render(input)
	footerW := m.width - 2*styles.InputMarginH
	if footerW < 1 {
		footerW = 1
	}
	footer := lipgloss.NewStyle().
		Margin(0, styles.InputMarginH).
		Render(inputFooter(footerW, m.ws, m.cfg, m.mode))

	palette := m.renderOverlay(m.width)
	if palette != "" {
		ph := lipgloss.Height(palette)
		main = clipBottomLines(main, ph)
	}
	parts := []string{main}
	if palette != "" {
		parts = append(parts, palette)
	}
	parts = append(parts, "", input, footer)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return m.programView(content)
}

func (m Model) programView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	// Enables shift+enter and other modified keys on supporting terminals.
	v.KeyboardEnhancements.ReportEventTypes = true
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
