package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/permission"
	"github.com/axispx/zeta/internal/search"
	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/skill"
	"github.com/axispx/zeta/internal/styles"
)

// command is a slash-palette entry. skill marks a bundled playbook binding
// (fill-into-input); harness commands run immediately via runCommand.
type command struct {
	name  string
	desc  string
	skill bool
}

// builtinCommands are harness commands (not skill slash bindings).
var builtinCommands = []command{
	{name: "/clear", desc: "start a new session"},
	{name: "/compact", desc: "summarize older context"},
	{name: "/resume", desc: "open a previous session"},
	{name: "/model", desc: "switch model"},
	{name: "/config", desc: "manage providers & models"},
}

// commands is builtins plus slash-bound bundled skills (init-time, fixed).
// Harness tokens win: a skill slash that collides with a builtin panics at startup.
var commands []command

func init() {
	commands = make([]command, 0, len(builtinCommands)+len(skill.All()))
	seen := make(map[string]struct{}, len(builtinCommands)+len(skill.All()))
	for _, c := range builtinCommands {
		commands = append(commands, c)
		seen[c.name] = struct{}{}
	}
	for _, s := range skill.All() {
		if s.Slash == "" {
			continue
		}
		if _, clash := seen[s.Slash]; clash {
			panic(fmt.Sprintf("skill slash %q collides with a harness command", s.Slash))
		}
		commands = append(commands, command{name: s.Slash, desc: s.Description, skill: true})
		seen[s.Slash] = struct{}{}
	}
}

// listSel is shared selection state for overlays.
type listSel struct {
	selected int
}

func (l *listSel) clear() { l.selected = 0 }

func (l *listSel) clamp(n int) {
	if n <= 0 {
		l.selected = 0
		return
	}
	if l.selected >= n {
		l.selected = n - 1
	}
	if l.selected < 0 {
		l.selected = 0
	}
}

// move adjusts selection for a list of length n. Returns whether key was a nav key.
func (l *listSel) move(n int, key string) bool {
	switch key {
	case "up", "ctrl+p":
		if l.selected > 0 {
			l.selected--
		}
		return true
	case "down", "ctrl+n":
		if n > 0 && l.selected < n-1 {
			l.selected++
		}
		return true
	}
	return false
}

type overlayMode int

const (
	overlayOff overlayMode = iota
	overlayCommands
	overlayModels
)

const modelOverlayMaxRows = 5

// filterOverlay is the inline list above the input (slash commands or model picker).
type filterOverlay struct {
	mode overlayMode
	listSel
	cmds   []command            // overlayCommands
	models []config.ModelChoice // overlayModels catalog
}

func (o *filterOverlay) clear() {
	o.mode = overlayOff
	o.cmds = nil
	o.models = nil
	o.listSel.clear()
}

func (o *filterOverlay) showing() bool {
	switch o.mode {
	case overlayCommands:
		return len(o.cmds) > 0
	case overlayModels:
		return true
	default:
		return false
	}
}

func modelChoiceHaystack(c config.ModelChoice) string {
	return c.Name + " " + c.ID()
}

func (o *filterOverlay) visibleModels(query string) []config.ModelChoice {
	return search.Filter(query, o.models, modelChoiceHaystack)
}

func commandHaystack(c command) string { return c.name + " " + c.desc }

func matchCommands(prefix string) []command {
	if !strings.HasPrefix(prefix, "/") {
		return nil
	}
	return search.Filter(strings.TrimPrefix(prefix, "/"), commands, commandHaystack)
}

func lookupCommand(name string) (command, bool) {
	name = strings.TrimSpace(name)
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

func isSlashToken(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "/") {
		return false
	}
	return !strings.ContainsAny(s, " \t\n")
}

func (m *Model) resetInput() {
	m.textarea.Reset()
	m.textarea.SetHeight(inputMinHeight)
	m.syncTextareaStyles()
	m.resetPromptHistory()
}

func (m *Model) applyClient() {
	choice, ok := m.cfg.ActiveChoice()
	if !ok {
		m.client = nil
		return
	}
	p, ok := m.cfg.Provider(choice.ProviderID)
	if !ok {
		m.client = nil
		return
	}
	m.client = ai.New(p, choice.ModelID)
}

// ensureFreshClient refreshes OAuth tokens if needed, then rebuilds the client.
func (m *Model) ensureFreshClient() error {
	choice, ok := m.cfg.ActiveChoice()
	if !ok {
		return nil
	}
	refreshed, err := m.cfg.EnsureOAuthFresh(context.Background(), choice.ProviderID)
	if err != nil {
		return err
	}
	if refreshed {
		m.applyClient()
	}
	return nil
}

func (m *Model) syncOverlay() {
	before := m.gapHeight()
	defer func() {
		if m.ready && m.gapHeight() != before {
			m.layoutPreservingBottom()
		}
	}()
	if m.picker.active || m.config.active {
		m.overlay.clear()
		return
	}
	if m.overlay.mode == overlayModels {
		m.overlay.clamp(len(m.overlay.visibleModels(m.textarea.Value())))
		return
	}
	val := m.textarea.Value()
	if !strings.HasPrefix(val, "/") || strings.ContainsAny(val, " \t\n") {
		m.overlay.clear()
		return
	}
	items := matchCommands(val)
	if len(items) == 0 {
		m.overlay.clear()
		return
	}
	m.overlay.mode = overlayCommands
	m.overlay.cmds = items
	m.overlay.models = nil
	m.overlay.clamp(len(items))
}

func (m *Model) dismissOverlay() {
	m.overlay.clear()
	m.resetInput()
	if m.ready {
		m.layoutPreservingBottom()
	}
}

func (m *Model) runCommand(name string) tea.Cmd {
	m.finishTurn()
	m.resetInput()
	m.overlay.clear()

	switch name {
	case "/clear":
		m.startNewSession()
	case "/compact":
		return m.startCompact()
	case "/resume":
		m.openPicker()
	case "/model":
		m.openModelOverlay()
	case "/config":
		return m.openConfigDialog()
	}
	return nil
}

// fillSkillSlash puts a skill token in the input (trailing space for args) and
// dismisses the command overlay without submitting.
func (m *Model) fillSkillSlash(name string) {
	m.overlay.clear()
	m.textarea.SetValue(name + " ")
	m.textarea.MoveToEnd()
	if m.ready {
		m.layoutPreservingBottom()
	}
}

func (m *Model) openConfigDialog() tea.Cmd {
	m.overlay.clear()
	m.picker.clear()
	m.config.apply = func(c config.Config) {
		m.cfg = c
		m.applyClient()
	}
	return m.config.Open(m.cfg)
}

func (m *Model) applySession(sess *session.Session, recs []session.Record, err error) {
	if err != nil {
		m.messages = []Message{{Role: RoleError, Text: "session: " + err.Error()}}
		m.sess = nil
		m.history = nil
	} else {
		m.sess = sess
		m.messages, m.history = loadSession(recs)
	}
	m.contextTokens = 0
	m.titlePending = false
	m.clearCompactState()
	m.resetPromptHistory()
	m.clearBottom()
	m.pendingPlan = ""
	m.overlay.clear()
	m.grants = &permission.Session{}
	m.tx.invalidate()
	m.refreshTranscript()
}

func (m *Model) startNewSession() {
	sess, err := session.New(m.ws.Abs)
	m.applySession(sess, nil, err)
}

func (m *Model) openModelOverlay() {
	entries := m.cfg.ModelChoices()
	if len(entries) == 0 {
		m.messages = append(m.messages, Message{Role: RoleSystem, Text: "no models configured"})
		m.refreshTranscript()
		return
	}
	m.overlay.clear()
	m.overlay.mode = overlayModels
	m.overlay.models = entries
	m.resetInput()
	active := m.cfg.Active
	for i, e := range entries {
		if e.ID() == active {
			m.overlay.selected = i
			break
		}
	}
	if m.ready {
		m.layoutPreservingBottom()
	}
}

func (m *Model) selectModel() {
	if m.overlay.mode != overlayModels {
		return
	}
	visible := m.overlay.visibleModels(m.textarea.Value())
	if len(visible) == 0 {
		return
	}
	choice := visible[m.overlay.selected]

	prevCfg := m.cfg
	prevClient := m.client

	m.cfg.SetActive(choice.ID())
	if err := m.cfg.Save(); err != nil {
		m.cfg = prevCfg
		m.client = prevClient
		m.dismissOverlay()
		m.messages = append(m.messages, Message{Role: RoleError, Text: "config save: " + err.Error()})
		m.refreshTranscript()
		return
	}
	m.contextTokens = 0
	m.applyClient()
	m.dismissOverlay()
	m.refreshTranscript()
}

func (m *Model) handleOverlayKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch m.overlay.mode {
	case overlayModels:
		n := len(m.overlay.visibleModels(m.textarea.Value()))
		if m.overlay.move(n, key) {
			return nil, true
		}
		switch key {
		case "enter":
			m.selectModel()
			return nil, true
		case "esc":
			m.dismissOverlay()
			return nil, true
		}
		return nil, false
	case overlayCommands:
		if !m.overlay.showing() {
			return nil, false
		}
		if m.overlay.move(len(m.overlay.cmds), key) {
			return nil, true
		}
		if key == "tab" {
			cmd := m.overlay.cmds[m.overlay.selected]
			if cmd.skill {
				m.fillSkillSlash(cmd.name)
			} else {
				m.textarea.SetValue(cmd.name)
			}
			return nil, true
		}
		return nil, false
	default:
		return nil, false
	}
}

// consumeCommandOverlayKey handles nav/tab for the slash-command overlay.
func (m *Model) consumeCommandOverlayKey(msg tea.KeyPressMsg) bool {
	if m.overlay.mode != overlayCommands {
		return false
	}
	_, ok := m.handleOverlayKey(msg)
	return ok
}

// submitInput handles plain Enter: selected palette command, exact slash command, or chat.
func (m *Model) submitInput() tea.Cmd {
	if m.busy() {
		return nil
	}
	if m.overlay.mode == overlayCommands && m.overlay.showing() {
		cmd := m.overlay.cmds[m.overlay.selected]
		// Skills always fill so the user can add args; second Enter submits.
		if cmd.skill {
			m.fillSkillSlash(cmd.name)
			return nil
		}
		return m.runCommand(cmd.name)
	}
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return nil
	}
	if text == ":q" { // vim
		return m.requestQuit()
	}
	// Skill slash (exact or with args) → chat turn; playbook injected in requestMsgs.
	if _, ok := skill.MatchSlash(text); ok {
		return m.submit(text)
	}
	if isSlashToken(text) {
		if c, ok := lookupCommand(text); ok && !c.skill {
			return m.runCommand(text)
		}
		m.resetInput()
		m.overlay.clear()
		m.messages = append(m.messages, Message{Role: RoleError, Text: "unknown command: " + text})
		m.refreshTranscript()
		return nil
	}
	return m.submit(text)
}

func clipBottomLines(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return ""
	}
	return strings.Join(lines[:len(lines)-n], "\n")
}

// windowAround returns a [start,end) window of size listH centered on selected.
func windowAround(selected, n, listH int) (start, end int) {
	if n <= listH {
		return 0, n
	}
	start = selected - listH/2
	if start < 0 {
		start = 0
	}
	end = start + listH
	if end > n {
		end = n
		start = end - listH
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func paletteNameWidth(items []command) int {
	max := 0
	for _, c := range items {
		if w := lipgloss.Width(c.name); w > max {
			max = w
		}
	}
	return max
}

func formatPaletteRow(nameW int, c command, selected bool, ink styles.OverlayInk) string {
	prefix := strings.Repeat(" ", inputPromptWidth)
	labelStyle, hintStyle := ink.Row, ink.Hint
	if selected {
		prefix = inputPrompt
		labelStyle, hintStyle = ink.Selected, ink.SelectedHint
	}
	nameCol := labelStyle.Width(inputPromptWidth + nameW).Render(prefix + c.name)
	return nameCol + ink.Gap.Render("  ") + hintStyle.Render(c.desc)
}

func (m Model) renderOverlay(width int) string {
	switch m.overlay.mode {
	case overlayCommands:
		return m.renderCommandOverlay(width)
	case overlayModels:
		return m.renderModelOverlay(width)
	default:
		return ""
	}
}

func (m Model) renderCommandOverlay(width int) string {
	if !m.overlay.showing() {
		return ""
	}
	innerW, contentW := overlayWidths(width)
	ink := m.chrome.OverlayInk()
	nameW := paletteNameWidth(m.overlay.cmds)
	var b strings.Builder
	for i, c := range m.overlay.cmds {
		if i > 0 {
			b.WriteByte('\n')
		}
		row := formatPaletteRow(nameW, c, i == m.overlay.selected, ink)
		if contentW > 0 {
			row = ink.Gap.Width(contentW).Render(row)
		}
		b.WriteString(row)
	}
	return m.paintOverlay(b.String(), innerW)
}

// formatHintRow renders "prefix+label … hint" within innerW.
func formatHintRow(prefix, label, hint string, innerW int, labelStyle, hintStyle, gap lipgloss.Style) string {
	return formatHintRowTagged(prefix, label, "", hint, innerW, labelStyle, hintStyle, gap)
}

// formatHintRowTagged is formatHintRow with an optional dim tag after the label (e.g. " (Custom)").
func formatHintRowTagged(prefix, label, tag, hint string, innerW int, labelStyle, hintStyle, gap lipgloss.Style) string {
	if innerW < 1 {
		innerW = 1
	}
	hintR := ""
	hintW := 0
	if hint != "" {
		hintR = hintStyle.Render(hint)
		hintW = lipgloss.Width(hintR)
	}
	tagW := lipgloss.Width(tag)
	// Reserve space for hint + at least one gap column when hint is present.
	gapMin := 0
	if hintW > 0 {
		gapMin = 1
	}
	maxLeft := innerW - hintW - gapMin
	if maxLeft < 1 {
		maxLeft = 1
	}
	avail := maxLeft - lipgloss.Width(prefix) - tagW
	if avail < 1 {
		avail = 1
	}
	if lipgloss.Width(label) > avail {
		label = truncateRight(label, avail)
	}
	leftR := labelStyle.Render(prefix + label)
	if tag != "" {
		// Same style as the name so "(Custom)" reads as part of the label.
		leftR += labelStyle.Render(tag)
	}
	pad := innerW - lipgloss.Width(leftR) - hintW
	if pad < gapMin {
		pad = gapMin
	}
	if hintW == 0 {
		return leftR
	}
	return leftR + gap.Render(strings.Repeat(" ", pad)) + hintR
}

// formatAccentRow renders a selected/current accent list row ("→ label … hint").
// current wins color over selected; selected still gets the arrow.
func formatAccentRow(label, hint string, innerW int, selected, current bool, ink styles.OverlayInk) string {
	return formatAccentRowTagged(label, "", hint, innerW, selected, current, ink)
}

// formatAccentRowTagged is formatAccentRow with an optional dim tag after the label.
func formatAccentRowTagged(label, tag, hint string, innerW int, selected, current bool, ink styles.OverlayInk) string {
	prefix := strings.Repeat(" ", inputPromptWidth)
	if selected {
		prefix = inputPrompt
	}
	labelStyle, hintStyle := ink.Row, ink.Hint
	switch {
	case current:
		labelStyle, hintStyle = ink.Current, ink.CurrentHint
	case selected:
		labelStyle, hintStyle = ink.Selected, ink.SelectedHint
	}
	return formatHintRowTagged(prefix, label, tag, hint, innerW, labelStyle, hintStyle, ink.Gap)
}

func (m Model) renderModelOverlay(width int) string {
	visible := m.overlay.visibleModels(m.textarea.Value())
	if m.overlay.mode != overlayModels || len(visible) == 0 {
		return ""
	}

	innerW, contentW := overlayWidths(width)
	ink := m.chrome.OverlayInk()
	// Drop leading newline from shared list helper (overlay has no header above).
	body := strings.TrimPrefix(
		renderModelChoiceList(visible, m.overlay.selected, m.cfg.Active, "active", contentW, modelOverlayMaxRows, ink),
		"\n",
	)
	return m.paintOverlay(body, innerW)
}

// overlayWidths returns panel total width and content width (excludes right pad).
func overlayWidths(termW int) (innerW, contentW int) {
	innerW = termW - 2*styles.InputMarginH
	if innerW < 1 {
		innerW = 1
	}
	contentW = innerW - styles.OverlayPadRight
	if contentW < 1 {
		contentW = 1
	}
	return innerW, contentW
}

// paintOverlay fills the list with panel chrome so it doesn't blend into the transcript.
func (m Model) paintOverlay(body string, innerW int) string {
	return lipgloss.NewStyle().
		Margin(0, styles.InputMarginH).
		Render(m.chrome.OverlayPanel().Width(innerW).Render(body))
}
