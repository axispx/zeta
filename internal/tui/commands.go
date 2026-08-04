package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/image"
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
// j/k are intentionally omitted: filter overlays still receive typed characters.
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
	overlayFiles
)

const modelOverlayMaxRows = 5

// filterOverlay is the inline list above the input (slash / model / @ files).
type filterOverlay struct {
	mode overlayMode
	listSel
	cmds   []command            // overlayCommands
	models []config.ModelChoice // overlayModels catalog
	files  filePicker           // overlayFiles only
}

func (o *filterOverlay) clear() {
	o.mode = overlayOff
	o.cmds = nil
	o.models = nil
	o.files.clear()
	o.listSel.clear()
}

// ownsInput reports pickers where the composer text is the filter query
// (slash / model). @ mentions edit a larger draft and must not wipe it on close.
func (o filterOverlay) ownsInput() bool {
	return o.mode == overlayCommands || o.mode == overlayModels
}

func (o *filterOverlay) showing() bool {
	switch o.mode {
	case overlayCommands:
		return len(o.cmds) > 0
	case overlayModels:
		return true
	case overlayFiles:
		// Empty matches / still loading with no rows: hide list, keep inventory
		// for sync refilter. Keys fall through (Enter submits, Tab types).
		return o.files.visible()
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

// commandPrefixKey is the slash token without leading '/' (prefix match target).
func commandPrefixKey(c command) string { return strings.TrimPrefix(c.name, "/") }

func matchCommands(prefix string) []command {
	if !strings.HasPrefix(prefix, "/") {
		return nil
	}
	// Prefix on the command name only — small fixed vocabulary; fuzzy subsequence
	// on desc ("new" → /clear) is more surprising than helpful.
	return search.Prefix(strings.TrimPrefix(prefix, "/"), commands, 0, commandPrefixKey)
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
	m.clearPendingImages()
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

func (m *Model) syncOverlay() tea.Cmd {
	// Filter overlays float (no layout height); gap stays idle blank / status.
	if m.picker.active || m.config.active {
		m.closeOverlay()
		return nil
	}
	if m.overlay.mode == overlayModels {
		m.overlay.clamp(len(m.overlay.visibleModels(m.textarea.Value())))
		return nil
	}
	val := m.textarea.Value()
	// Whole-input slash palette wins over @ mentions.
	if strings.HasPrefix(val, "/") && !strings.ContainsAny(val, " \t\n") {
		items := matchCommands(val)
		if len(items) == 0 {
			m.closeOverlay()
			return nil
		}
		// Drop file inventory when leaving @ mode.
		if m.overlay.mode != overlayCommands {
			m.closeOverlay()
		}
		m.overlay.mode = overlayCommands
		m.overlay.cmds = items
		m.overlay.clamp(len(items))
		return nil
	}
	if tok, ok := atTokenAtCursor(val, m.textarea.Line(), m.textarea.Column()); ok {
		return m.syncFileOverlay(tok.query)
	}
	m.closeOverlay()
	return nil
}

// syncFileOverlay keeps the @ picker in sync with the current query.
// Lists the workspace once (async); filters sync on each keystroke after that.
func (m *Model) syncFileOverlay(query string) tea.Cmd {
	f := &m.overlay.files
	if m.overlay.mode != overlayFiles {
		// Entering @ mode: wipe slash/model state; selection resets via clear.
		m.closeOverlay()
		m.overlay.mode = overlayFiles
	}
	if f.query != query {
		f.query = query
		if f.all != nil {
			m.refilterFiles()
		}
	} else {
		m.overlay.clamp(len(f.matches))
	}
	return m.ensureFileList()
}

// closeOverlay clears filter-overlay state without touching the composer.
// Cancels any in-flight @ file list (via filePicker.clear).
func (m *Model) closeOverlay() {
	m.overlay.clear()
}

// cancelOverlay closes the active filter overlay (Esc / Ctrl+C rung).
// Slash/model own the input as their query, so cancel wipes it; @ keeps the draft.
func (m *Model) cancelOverlay() {
	owns := m.overlay.ownsInput()
	m.closeOverlay()
	if owns {
		m.resetInput()
		if m.ready {
			m.layoutPreservingBottom()
		}
	}
}

func (m *Model) runCommand(name string) tea.Cmd {
	m.finishTurn() // no-op when idle
	m.resetInput()
	m.closeOverlay()

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
	m.closeOverlay()
	m.textarea.SetValue(name + " ")
	m.textarea.MoveToEnd()
	if m.ready {
		m.layoutPreservingBottom()
	}
}

func (m *Model) openConfigDialog() tea.Cmd {
	m.closeOverlay()
	m.picker.clear()
	return m.config.Open(m.cfg)
}

// updateConfigDialog forwards a msg to the dialog and collects anything it
// saved, so a write reaches the Model that Update actually returns.
func (m *Model) updateConfigDialog(msg tea.Msg) (tea.Cmd, bool) {
	cmd, handled := m.config.Update(msg)
	if c := m.config.takeSaved(); c != nil {
		m.cfg = *c
		m.applyClient()
	}
	return cmd, handled
}

func (m *Model) applySession(sess *session.Session, recs []session.Record, err error) {
	if err != nil {
		m.messages = []Message{{Role: RoleError, Text: "session: " + err.Error()}}
		m.sess = nil
		m.history = nil
		m.seedTodos(nil)
	} else {
		m.sess = sess
		m.messages, m.history = loadSession(recs)
		m.seedTodos(todosFromRecords(recs))
	}
	m.refreshSessionDiff()
	m.contextTokens = 0
	m.titlePending = false
	m.clearCompactState()
	m.resetPromptHistory()
	m.clearBottom()
	m.pendingPlan = ""
	m.clearQueue()
	m.closeOverlay()
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
	m.closeOverlay()
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
		m.cancelOverlay()
		m.messages = append(m.messages, Message{Role: RoleError, Text: "config save: " + err.Error()})
		m.refreshTranscript()
		return
	}
	m.contextTokens = 0
	m.applyClient()
	m.cancelOverlay()
	m.refreshTranscript()
}

// handleOverlayKey handles nav/tab/enter for every visible filter overlay.
// Returns (cmd, true) when the key is consumed. Hidden overlays (e.g. @ with
// no matches) return false so keys reach the composer / submitInput.
func (m *Model) handleOverlayKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.overlay.showing() {
		return nil, false
	}
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
			m.cancelOverlay()
			return nil, true
		}
		return nil, false
	case overlayCommands:
		if m.overlay.move(len(m.overlay.cmds), key) {
			return nil, true
		}
		switch key {
		case "tab":
			cmd := m.overlay.cmds[m.overlay.selected]
			if cmd.skill {
				m.fillSkillSlash(cmd.name)
			} else {
				m.textarea.SetValue(cmd.name)
			}
			return nil, true
		case "enter":
			// Busy turn: consume Enter so the slash is not queued as chat.
			if m.turn != nil {
				return nil, true
			}
			cmd := m.overlay.cmds[m.overlay.selected]
			// Skills always fill so the user can add args; second Enter submits.
			if cmd.skill {
				m.fillSkillSlash(cmd.name)
				return nil, true
			}
			return m.runCommand(cmd.name), true
		}
		return nil, false
	case overlayFiles:
		if m.overlay.move(len(m.overlay.files.matches), key) {
			return nil, true
		}
		switch key {
		case "enter", "tab":
			m.insertFileMention()
			return nil, true
		}
		return nil, false
	default:
		return nil, false
	}
}

// submitInput handles plain Enter: slash, save-edit, queue, or drain.
// Overlay commits are handled in handleOverlayKey before this runs.
// Empty Enter delivers the queue head now (interrupts a live turn when busy).
func (m *Model) submitInput() tea.Cmd {
	// Compact blocks all submit; auth recover queues like a live turn.
	if m.compacting {
		return nil
	}

	text, imgs := m.parseComposer()
	if text == "" && len(imgs) == 0 {
		// Empty Enter while editing: keep the item; user must save or Esc.
		if m.editID != 0 {
			return nil
		}
		// Do not interrupt/replace an in-flight OAuth recover.
		if m.authRetrying {
			return nil
		}
		return m.drainNextQueuedPrompt()
	}
	// Saving an open follow-up beats slash / turn routing.
	if m.editID != 0 {
		m.saveEdit(text, imgs)
		return nil
	}
	if m.turn == nil && !m.authRetrying && text == ":q" { // vim
		return m.requestQuit()
	}

	// Non-skill slash → harness policy; skill slash is chat content.
	if isSlashToken(text) {
		if _, ok := skill.MatchSlash(text); !ok {
			return m.submitHarnessSlash(text, imgs)
		}
	}
	// Mid-turn / OAuth recover: queue for later. Send-now is empty Enter /
	// queue-focus Enter (blocked while authRetrying).
	if m.turn != nil || m.authRetrying {
		return m.enqueuePrompt(text, imgs)
	}
	return m.submit(text, imgs)
}

// submitHarnessSlash runs a non-skill slash command, or rejects it when idle.
// Mid-turn / OAuth recover: all harness/unknown slashes are swallowed.
func (m *Model) submitHarnessSlash(text string, imgs []image.Ref) tea.Cmd {
	if len(imgs) > 0 {
		m.noteSystem("slash commands cannot include images")
		return nil
	}
	if m.turn != nil || m.authRetrying {
		return nil
	}
	if c, ok := lookupCommand(text); ok && !c.skill {
		return m.runCommand(text)
	}
	m.resetInput()
	m.closeOverlay()
	m.noteError("unknown command: " + text)
	return nil
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
	case overlayFiles:
		return m.renderFileOverlay(width)
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
