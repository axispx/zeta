package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/codex"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/harness"
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
	{name: "/usage", desc: "session token usage"},
	{name: "/resume", desc: "open a previous session"},
	{name: "/model", desc: "switch model"},
	{name: "/fast", desc: "toggle Fast mode"},
	{name: "/config", desc: "manage providers & models"},
	{name: "/update", desc: "update to latest version"},
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
func (o *filterOverlay) ownsInput() bool {
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

// isSkillSlash reports a message that invokes a bundled skill (with or without args).
func isSkillSlash(text string) bool {
	if !strings.HasPrefix(strings.TrimSpace(text), "/") {
		return false
	}
	_, ok := skill.MatchSlash(text)
	return ok
}

func (m *Model) resetInput() {
	m.composer.textarea.Reset()
	m.composer.textarea.SetHeight(inputMinHeight)
	m.syncTextareaStyles()
	m.resetPromptHistory()
	m.clearPendingImages()
	m.queue.recalled = nil
}

func (m *Model) syncOverlay() tea.Cmd {
	// Filter overlays float (no layout height); gap stays idle blank / status.
	if m.picker.active || m.config.active {
		m.closeOverlay()
		return nil
	}
	if m.overlay.mode == overlayModels {
		m.overlay.clamp(len(m.overlay.visibleModels(m.composer.textarea.Value())))
		return nil
	}
	val := m.composer.textarea.Value()
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
	if tok, ok := atTokenAtCursor(val, m.composer.textarea.Line(), m.composer.textarea.Column()); ok {
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
		if m.term.ready {
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
	case "/usage":
		return m.reportUsage()
	case "/resume":
		m.openPicker()
	case "/model":
		m.openModelOverlay()
	case "/fast":
		return m.toggleFast()
	case "/config":
		return m.openConfigDialog()
	case "/update":
		return m.requestUpdate()
	}
	return nil
}

// fillSkillSlash puts a skill token in the input (trailing space for args) and
// dismisses the command overlay without submitting.
func (m *Model) fillSkillSlash(name string) {
	m.closeOverlay()
	m.composer.textarea.SetValue(name + " ")
	m.composer.textarea.MoveToEnd()
	if m.term.ready {
		m.layoutPreservingBottom()
	}
}

func (m *Model) openConfigDialog() tea.Cmd {
	m.closeOverlay()
	m.picker.clear()
	return m.config.Open(m.session.Cfg)
}

// updateConfigDialog forwards a msg to the dialog and collects anything it
// saved, so a write reaches the Model that Update actually returns.
func (m *Model) updateConfigDialog(msg tea.Msg) (tea.Cmd, bool) {
	cmd, handled := m.config.Update(msg)
	if c := m.config.takeSaved(); c != nil {
		m.session.Cfg = *c
		m.session.ApplyClient()
		// A saved model change switches the prefix (and the provider cache).
		m.session.ResetContext()
		m.session.ReconcileCompaction()
	}
	return cmd, handled
}

func (m *Model) applySession(sess *session.Session, recs []session.Record, err error) {
	if err != nil {
		m.transcript.messages = []Message{{Role: RoleError, Text: "session: " + err.Error()}}
		m.session.Log = nil
		m.session.History = nil
		m.session.SeedTodos(nil)
	} else {
		m.session.Log = sess
		m.transcript.messages, m.session.History = loadSession(recs, m.session.NativeModel())
		m.session.SeedTodos(harness.TodosFromRecords(recs))
	}
	// A session boundary is when project instructions are read: /clear and
	// /resume pick up an edited AGENTS.md, turns in between do not.
	m.session.ReloadAgents()
	m.session.ResetContext()
	// /resume replays the persisted per-turn accounting; /clear starts at zero.
	m.session.Usage = harness.UsageFromRecords(recs)
	m.session.TitlePending = false
	m.clearCompactState()
	m.resetPromptHistory()
	m.clearPanel()
	m.clearQueue()
	m.closeOverlay()
	m.session.Grants = &permission.Session{}
	m.transcript.invalidate()
	m.refreshTranscript()
}

func (m *Model) startNewSession() {
	sess, err := session.New(m.session.WS.Abs)
	m.applySession(sess, nil, err)
}

func (m *Model) openModelOverlay() {
	entries := m.session.Cfg.ModelChoices()
	if len(entries) == 0 {
		m.transcript.messages = append(m.transcript.messages, Message{Role: RoleSystem, Text: "no models configured"})
		m.refreshTranscript()
		return
	}
	m.closeOverlay()
	m.overlay.mode = overlayModels
	m.overlay.models = entries
	m.resetInput()
	active := m.session.Cfg.Active
	for i, e := range entries {
		if e.ID() == active {
			m.overlay.selected = i
			break
		}
	}
	if m.term.ready {
		m.layoutPreservingBottom()
	}
}

func (m *Model) selectModel() {
	if m.overlay.mode != overlayModels {
		return
	}
	visible := m.overlay.visibleModels(m.composer.textarea.Value())
	if len(visible) == 0 {
		return
	}
	choice := visible[m.overlay.selected]

	prevCfg := m.session.Cfg
	prevClient := m.session.Client

	m.session.Cfg.SetActive(choice.ID())
	if err := m.session.Cfg.Save(); err != nil {
		m.session.Cfg = prevCfg
		m.session.Client = prevClient
		m.cancelOverlay()
		m.transcript.messages = append(m.transcript.messages, Message{Role: RoleError, Text: "config save: " + err.Error()})
		m.refreshTranscript()
		return
	}
	m.session.ResetContext()
	m.session.ApplyClient()
	m.session.ReconcileCompaction()
	m.cancelOverlay()
	m.refreshTranscript()
}

// toggleFast flips fast mode on the active model and persists it. A Codex model
// with no tier recorded (connected before fast mode existed, or a stale model
// cache) first re-reads the account's catalog rather than making the user sign
// in again. The tier is a request field, not part of the cached prefix, so the
// context is kept.
func (m *Model) toggleFast() tea.Cmd {
	choice, ok := m.session.Cfg.ActiveChoice()
	if !ok {
		m.noteError("no active model")
		return nil
	}
	p, _ := m.session.Cfg.Provider(choice.ProviderID)
	if !m.session.Cfg.ActiveFast() && p.Models[choice.ModelID].FastTier == "" && codex.IsEndpoint(p.BaseURL) {
		return m.refreshFastTiers(choice)
	}
	m.setFast(choice, !m.session.Cfg.ActiveFast())
	return nil
}

// setFast applies and saves fast mode for choice, then reports it.
func (m *Model) setFast(choice config.ModelChoice, on bool) {
	if err := m.session.Cfg.SetFast(choice.ProviderID, choice.ModelID, on); err != nil {
		m.noteError(err.Error())
		return
	}
	if err := m.session.Cfg.Save(); err != nil {
		_ = m.session.Cfg.SetFast(choice.ProviderID, choice.ModelID, !on)
		m.noteError("config save: " + err.Error())
		return
	}
	m.session.ApplyClient()
	if on {
		m.noteSystem("Fast mode on for " + choice.Name + " (faster, uses more of your plan)")
	} else {
		m.noteSystem("Fast mode off")
	}
}

// fastTiersMsg is the outcome of re-reading the Codex model catalog for /fast.
type fastTiersMsg struct {
	choice config.ModelChoice
	models []codex.Model
	err    error
}

// refreshFastTiers reads the account's Codex catalog with the current token.
// The cmd works on a clone: Update must not share the live config with another
// goroutine.
func (m *Model) refreshFastTiers(choice config.ModelChoice) tea.Cmd {
	cfg := m.session.Cfg.Clone()
	return func() tea.Msg {
		p, _ := cfg.Provider(choice.ProviderID)
		var accountID string
		if p.OAuth != nil {
			accountID = p.OAuth.AccountID
		}
		models, err := codex.Models(context.Background(), p.AuthToken(), accountID)
		return fastTiersMsg{choice: choice, models: models, err: err}
	}
}

// handleFastTiers records the freshly read tiers, then turns fast mode on.
func (m *Model) handleFastTiers(msg fastTiersMsg) {
	if msg.err != nil {
		m.noteError("Fast mode: " + msg.err.Error())
		return
	}
	_ = codex.SaveModels(msg.models)
	tiers := make(map[string]string, len(msg.models))
	for _, cm := range msg.models {
		tiers[cm.Slug] = cm.FastTier
	}
	if err := m.session.Cfg.SetFastTiers(msg.choice.ProviderID, tiers); err != nil {
		m.noteError(err.Error())
		return
	}
	m.setFast(msg.choice, true)
}

// cycleModelReasoning walks the highlighted /model row's effort over the
// values that model accepts: off → first → … → last → off. Persists immediately.
func (m *Model) cycleModelReasoning() {
	if m.overlay.mode != overlayModels {
		return
	}
	visible := m.overlay.visibleModels(m.composer.textarea.Value())
	if len(visible) == 0 {
		return
	}
	choice := visible[m.overlay.selected]
	next := config.CycleReasoningEffort(choice.Effort, choice.Efforts)
	prev := choice.Effort
	if err := m.session.Cfg.SetReasoningEffort(choice.ProviderID, choice.ModelID, next); err != nil {
		m.transcript.messages = append(m.transcript.messages, Message{Role: RoleError, Text: err.Error()})
		m.refreshTranscript()
		return
	}
	if err := m.session.Cfg.Save(); err != nil {
		_ = m.session.Cfg.SetReasoningEffort(choice.ProviderID, choice.ModelID, prev)
		m.transcript.messages = append(m.transcript.messages, Message{Role: RoleError, Text: "config save: " + err.Error()})
		m.refreshTranscript()
		return
	}
	for i, e := range m.overlay.models {
		if e.ID() == choice.ID() {
			m.overlay.models[i].Effort = next
			break
		}
	}
	if m.session.Cfg.Active == choice.ID() {
		m.session.ApplyClient()
	}
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
		n := len(m.overlay.visibleModels(m.composer.textarea.Value()))
		if m.overlay.move(n, key) {
			return nil, true
		}
		switch key {
		case "tab":
			m.cycleModelReasoning()
			return nil, true
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
				m.composer.textarea.SetValue(cmd.name)
			}
			return nil, true
		case "enter":
			// Busy turn: consume Enter so the slash is not queued as chat.
			if m.turn.current != nil {
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

// submitInput handles plain Enter: slash, steer, or send. Overlay commits are
// handled in handleOverlayKey before this runs. Mid-turn, Enter steers: the
// message joins the running turn at its next tool boundary. Empty Enter while
// idle sends the oldest queued follow-up.
func (m *Model) submitInput() tea.Cmd {
	// Exclusive jobs block all submit; auth recover queues like a live turn.
	if m.exclusiveJob() {
		return nil
	}

	text, imgs := m.parseComposer()
	if text == "" && len(imgs) == 0 {
		if m.turn.current != nil {
			return nil
		}
		return m.sendQueued()
	}
	if m.turn.current == nil && !m.session.AuthRetrying && text == ":q" { // vim
		return m.requestQuit()
	}

	// Non-skill slash → harness policy; skill slash is chat content.
	if isSlashToken(text) {
		if _, ok := skill.MatchSlash(text); !ok {
			return m.submitHarnessSlash(text, imgs)
		}
	}
	skillSlash := isSkillSlash(text)
	// A skill playbook is attached when its turn starts, so it cannot join a
	// running turn: it waits in the queue instead. So does anything typed
	// during an OAuth recover.
	if m.session.AuthRetrying || (m.turn.current != nil && skillSlash) {
		return m.enqueuePrompt(text, imgs)
	}
	if m.turn.current != nil {
		return m.steerPrompt(text, imgs)
	}
	return m.submit(text, imgs)
}

// queueInput handles Tab mid-turn: hold the composer text as a follow-up that
// starts its own turn once this one ends.
func (m *Model) queueInput() tea.Cmd {
	if m.exclusiveJob() {
		return nil
	}
	text, imgs := m.parseComposer()
	if isSlashToken(text) {
		if _, ok := skill.MatchSlash(text); !ok {
			return nil
		}
	}
	return m.enqueuePrompt(text, imgs)
}

// submitHarnessSlash runs a non-skill slash command, or rejects it when idle.
// Mid-turn / OAuth recover: all harness/unknown slashes are swallowed.
func (m *Model) submitHarnessSlash(text string, imgs []image.Ref) tea.Cmd {
	if len(imgs) > 0 {
		m.noteSystem("slash commands cannot include images")
		return nil
	}
	if m.turn.current != nil || m.session.AuthRetrying {
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
	widest := 0
	for _, c := range items {
		if w := lipgloss.Width(c.name); w > widest {
			widest = w
		}
	}
	return widest
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

func (m *Model) renderOverlay(width int) string {
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

func (m *Model) renderCommandOverlay(width int) string {
	if !m.overlay.showing() {
		return ""
	}
	innerW, contentW := overlayWidths(width)
	ink := m.term.chrome.OverlayInk()
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

func (m *Model) renderModelOverlay(width int) string {
	visible := m.overlay.visibleModels(m.composer.textarea.Value())
	if m.overlay.mode != overlayModels || len(visible) == 0 {
		return ""
	}

	innerW, contentW := overlayWidths(width)
	ink := m.term.chrome.OverlayInk()
	// Drop leading newline from shared list helper (overlay has no header above).
	body := strings.TrimPrefix(
		renderModelChoiceList(visible, m.overlay.selected, m.session.Cfg.Active, "active", contentW, modelOverlayMaxRows, ink),
		"\n",
	)
	return m.paintOverlay(body, innerW)
}

// overlayWidths returns panel total width and content width (excludes right pad).
func overlayWidths(termW int) (innerW, contentW int) {
	innerW = termW
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
func (m *Model) paintOverlay(body string, innerW int) string {
	return m.term.chrome.OverlayPanel().Width(innerW).Render(body)
}

// renderModelChoiceList paints a scrollable model list.
// markID + markHint label the preferred/active row.
func renderModelChoiceList(models []config.ModelChoice, selected int, markID, markHint string, contentW, maxRows int, ink styles.OverlayInk) string {
	if len(models) == 0 {
		return ""
	}
	listH := min(maxRows, len(models))
	start, end := windowAround(selected, len(models), listH)
	var b strings.Builder
	for i, e := range models[start:end] {
		b.WriteByte('\n')
		idx := start + i
		b.WriteString(formatAccentRow(e.Name, modelChoiceHint(e, markID, markHint), contentW, idx == selected, e.ID() == markID, ink))
	}
	return b.String()
}

// modelChoiceHint is "active" plus the stored reasoning level.
func modelChoiceHint(e config.ModelChoice, markID, markHint string) string {
	var parts []string
	if e.ID() == markID && markHint != "" {
		parts = append(parts, markHint)
	}
	if e.Effort != "" {
		parts = append(parts, config.ReasoningEffortLabel(e.Effort))
	}
	return strings.Join(parts, " · ")
}
