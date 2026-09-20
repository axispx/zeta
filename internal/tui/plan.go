package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/plan"
	"github.com/axispx/zeta/internal/prompt"
	"github.com/axispx/zeta/internal/styles"
)

// planAction is one choice on the plan approval panel.
type planAction int

const (
	planApprove planAction = iota
	planRevise
	planDiscard
)

type planOption struct {
	label  string
	action planAction
}

var planOptions = []planOption{
	{"Approve & build", planApprove},
	{"Revise", planRevise},
	{"Discard", planDiscard},
}

func planOptionRows() []optionRow {
	rows := make([]optionRow, len(planOptions))
	for i, o := range planOptions {
		rows[i] = optionRow{label: o.label}
	}
	return rows
}

const planBuildListMaxRows = 5

// planPrompt is the modal after Plan mode emits <proposed_plan>.
// Replaces the input until the user decides. Build-model pick is a separate
// panel (buildPickPrompt) — not a mode bit on this struct.
type planPrompt struct {
	body  string
	title string
	list  optionList
}

func newPlanPrompt(body, title string) *planPrompt {
	p := &planPrompt{body: body, title: title}
	p.list.setRows(planOptionRows())
	return p
}

// buildPickPrompt is the post-approve model list (exclusive panel).
// Holds plan body/title so cancel can restore approval, and confirm can hand off.
type buildPickPrompt struct {
	body     string
	title    string
	models   []config.ModelChoice
	selected int
}

// noteProducedPlan records a plan body from the just-finished assistant
// segment. Offered once on turnDone via maybeOfferPlan (not re-scanned from
// history, so discard/revise do not re-open a stale plan).
func (m *Model) noteProducedPlan(asstText string) {
	if body, ok := m.session.ProducedPlan(asstText); ok {
		m.pendingPlan = body
	}
}

// maybeOfferPlan opens the approval panel when this turn produced a plan.
func (m *Model) maybeOfferPlan() {
	body := strings.TrimSpace(m.pendingPlan)
	m.pendingPlan = ""
	if body == "" || m.session.Mode != prompt.ModePlan || m.panel.plan != nil || m.panel.build != nil || m.turn.current != nil {
		return
	}
	m.panel.setPlan(newPlanPrompt(body, plan.Title(body)))
	m.closeOverlay()
	m.resetInput()
	m.afterPanelChange()
}

func (m *Model) dismissPlan() {
	m.panel.clear()
	m.pendingPlan = ""
	m.afterPanelChange()
}

// handlePlanKey consumes nav / row numbers / enter while the plan approval
// panel is open. A stray letter is swallowed; ↑/↓ or a row number moves,
// Enter confirms.
// Esc returns false so Update's interrupt path runs.
func (m *Model) handlePlanKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.panel.plan
	if p == nil {
		return nil, false
	}
	idx, chose, handled := p.list.handleKey(msg)
	if !handled {
		return nil, false
	}
	if chose && idx >= 0 && idx < len(planOptions) {
		return m.decidePlan(planOptions[idx].action), true
	}
	return nil, true
}

func (m *Model) decidePlan(action planAction) tea.Cmd {
	if m.panel.plan == nil {
		return nil
	}
	switch action {
	case planApprove:
		return m.beginPlanBuildPick()
	case planRevise:
		m.dismissPlan()
		// Stay in Plan; the next complete <proposed_plan> replaces the prior.
		m.noteSystem("Revise the plan — say what to change.")
		return nil
	case planDiscard:
		m.dismissPlan()
		m.noteSystem("Plan discarded.")
		return nil
	default:
		return nil
	}
}

// beginPlanBuildPick opens the build-model list as its own panel.
func (m *Model) beginPlanBuildPick() tea.Cmd {
	p := m.panel.plan
	if p == nil {
		return nil
	}
	entries := m.session.Cfg.ModelChoices()
	if len(entries) == 0 {
		m.dismissPlan()
		m.noteError("no models configured")
		return nil
	}
	sel := 0
	prefer := m.session.Cfg.PreferredBuildModel()
	for i, e := range entries {
		if e.ID() == prefer {
			sel = i
			break
		}
	}
	m.panel.setBuild(&buildPickPrompt{
		body:     p.body,
		title:    p.title,
		models:   entries,
		selected: sel,
	})
	m.resetInput()
	m.afterPanelChange()
	return nil
}

// cancelPlanBuildPick returns from build-model pick to approval options.
func (m *Model) cancelPlanBuildPick() {
	b := m.panel.build
	if b == nil {
		return
	}
	m.panel.setPlan(newPlanPrompt(b.body, b.title))
	m.resetInput()
	m.afterPanelChange()
}

// confirmPlanBuild switches to Build with modelID and starts implementing.
func (m *Model) confirmPlanBuild(modelID string) tea.Cmd {
	b := m.panel.build
	if b == nil {
		return nil
	}
	return m.beginBuildFromPlan(b.body, b.title, modelID)
}

// beginBuildFromPlan is the atomic plan→build handoff: persist preferred model,
// clear plan UI, new Build session, seed implement prompt.
func (m *Model) beginBuildFromPlan(body, title, modelID string) tea.Cmd {
	prevCfg := m.session.Cfg
	prevClient := m.session.Client
	m.session.Cfg.SetActive(modelID)
	m.session.Cfg.SetBuildDefault(modelID)
	if err := m.session.Cfg.Save(); err != nil {
		m.session.Cfg = prevCfg
		m.session.Client = prevClient
		m.dismissPlan()
		m.noteError("config save: " + err.Error())
		return nil
	}
	m.session.ResetContext()
	m.session.ApplyClient()

	m.panel.clear()
	m.pendingPlan = ""
	m.closeOverlay()
	m.resetInput()
	m.session.Mode = prompt.ModeBuild
	m.startNewSession()

	m.noteSystem("Building with " + m.session.Cfg.ModelName() + " · " + title)
	return m.submit(plan.BuildPrompt(body), nil)
}

// handlePlanClick selects an approval option under the cursor.
func (m *Model) handlePlanClick(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	p := m.panel.plan
	if p == nil || msg.Button != tea.MouseLeft {
		return nil, false
	}
	_, contentW := overlayWidths(m.term.width)
	idx, chose := p.list.handleClick(msg.X, msg.Y, m.transcript.viewport.Height(), m.term.width, m.planTitleH(), contentW)
	if !chose || idx < 0 || idx >= len(planOptions) {
		return nil, false
	}
	return m.decidePlan(planOptions[idx].action), true
}

func (m *Model) handlePlanMotion(msg tea.MouseMotionMsg) bool {
	p := m.panel.plan
	if p == nil {
		return false
	}
	_, contentW := overlayWidths(m.term.width)
	return p.list.handleMotion(msg.X, msg.Y, m.transcript.viewport.Height(), m.term.width, m.planTitleH(), contentW)
}

func (m Model) planTitleH() int {
	_, contentW := overlayWidths(m.term.width)
	ink := m.term.chrome.OverlayInk()
	return lipgloss.Height(m.renderPlanTitle(contentW, ink))
}

func (m Model) renderPlanApproval(width int) string {
	p := m.panel.plan
	if p == nil {
		return ""
	}
	_, contentW := overlayWidths(width)
	ink := m.term.chrome.OverlayInk()

	body := m.renderPlanTitle(contentW, ink) + p.list.render(contentW, ink)
	return renderPanelFrame(m.term.chrome, width, body)
}

func (m Model) renderPlanTitle(contentW int, ink styles.OverlayInk) string {
	inner := contentW - panelGutter
	if inner < 1 {
		inner = 1
	}
	title := m.panel.plan.title
	if title == "" {
		title = "Untitled plan"
	}
	line := ink.Header.Render("Approve plan · ") + ink.Gap.Render(truncateRight(title, max(1, inner-16)))
	return padPanel(ink.Gap.Width(inner).Render(line), panelGutter)
}

// --- build model pick (own panel) ---

func (m *Model) handleBuildPickKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	b := m.panel.build
	if b == nil {
		return nil, false
	}
	n := len(b.models)
	switch msg.String() {
	case "esc":
		return nil, false // interrupt path: cancelPlanBuildPick
	case "up", "ctrl+p":
		b.selected = moveOption(b.selected, n, -1)
		return nil, true
	case "down", "ctrl+n":
		b.selected = moveOption(b.selected, n, 1)
		return nil, true
	case "enter":
		if n == 0 {
			return nil, true
		}
		i := clampOption(b.selected, n)
		return m.confirmPlanBuild(b.models[i].ID()), true
	default:
		return nil, true // swallow
	}
}

func (m *Model) handleBuildPickClick(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	b := m.panel.build
	if b == nil || msg.Button != tea.MouseLeft {
		return nil, false
	}
	i := m.buildPickOptionAt(msg.X, msg.Y)
	if i < 0 || i >= len(b.models) {
		return nil, false
	}
	return m.confirmPlanBuild(b.models[i].ID()), true
}

func (m *Model) handleBuildPickMotion(msg tea.MouseMotionMsg) bool {
	b := m.panel.build
	if b == nil {
		return false
	}
	if i := m.buildPickOptionAt(msg.X, msg.Y); i >= 0 {
		b.selected = i
		return true
	}
	return false
}

func (m Model) buildPickOptionAt(x, y int) int {
	b := m.panel.build
	if b == nil || len(b.models) == 0 {
		return -1
	}
	_, contentW := overlayWidths(m.term.width)
	ink := m.term.chrome.OverlayInk()
	header := m.renderBuildPickHeader(contentW, ink)
	listH := min(planBuildListMaxRows, len(b.models))
	start, end := windowAround(b.selected, len(b.models), listH)
	idx := optionIndexAt(x, y, m.transcript.viewport.Height(), m.term.width, lipgloss.Height(header), end-start)
	if idx < 0 {
		return -1
	}
	return start + idx
}

func (m Model) renderBuildPickHeader(contentW int, ink styles.OverlayInk) string {
	b := m.panel.build
	title := "Build with"
	if b != nil {
		if t := strings.TrimSpace(b.title); t != "" {
			title = "Build with · " + truncateRight(t, max(1, contentW-14))
		}
	}
	return ink.Header.Width(contentW).Render(title) + "\n" +
		ink.Hint.Width(contentW).Render("Clears this chat and starts Build with the plan.")
}

// renderPlanBuildPick is the model list shown with input hidden after approve.
func (m Model) renderPlanBuildPick(width int) string {
	b := m.panel.build
	if b == nil || len(b.models) == 0 {
		return ""
	}
	_, contentW := overlayWidths(width)
	ink := m.term.chrome.OverlayInk()

	markID := m.session.Cfg.PreferredBuildModel()
	if markID == "" {
		markID = m.session.Cfg.Active
	}

	var sb strings.Builder
	sb.WriteString(m.renderBuildPickHeader(contentW, ink))
	sb.WriteString(renderModelChoiceList(b.models, b.selected, markID, "build", contentW, planBuildListMaxRows, ink))

	return renderPanelFrame(m.term.chrome, width, sb.String())
}

// renderModelChoiceList paints a scrollable model list (shared by plan build pick).
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

// modelChoiceHint is "active"/"build" plus the stored reasoning level.
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
