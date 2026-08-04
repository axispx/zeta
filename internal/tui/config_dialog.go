package tui

import (
	"image/color"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/models"
	"github.com/axispx/zeta/internal/oauth"
	"github.com/axispx/zeta/internal/search"
	"github.com/axispx/zeta/internal/styles"
)

type configView int

const (
	configPresets configView = iota
	configModels
	configFormView
	configAuth
)

type formBack int

const (
	backPresets formBack = iota
	backModels
	backModelsIfConfigured
	backAuth
)

const (
	configDialogListRows   = 12
	connectGroupConfigured = "configured"
	connectGroupProviders  = "providers"
)

// providerCaps is the action policy for a provider (catalog vs custom, oauth).
// Sourced from Provider.Custom and oauth.Supports — durable across catalog load failures.
type providerCaps struct {
	custom bool
	oauth  bool
}

func (c providerCaps) canRename() bool     { return c.custom }
func (c providerCaps) canEditModels() bool { return c.custom }
func (c providerCaps) canToggleAll() bool  { return !c.custom }
func (c providerCaps) apiKeyOnly() bool    { return !c.custom && !c.oauth }
func (c providerCaps) authChooser() bool   { return c.oauth }

// configDialog is the /config provider+model manager (self-contained sub-model).
type configDialog struct {
	active bool
	draft  config.Config
	// saved holds the last written config for the owning Model to collect.
	// A callback cannot work here: Model.Update takes a value receiver, so a
	// pointer captured when the dialog opened is stale by the next turn and
	// its writes land in a discarded copy.
	saved *config.Config

	view configView
	listSel
	focusID     string // provider being connected or edited
	modelID     string // model being edited
	presets     []config.Preset
	presetQuery string
	modelQuery  string
	loading     bool
	loadGen     int
	form        configForm
	status      string
	authTitle   string
	oauthGen    int
	oauth       *oauthSession
}

type configForm struct {
	title  string
	labels []string
	fields []textinput.Model
	focus  int
	back   formBack
	submit func(*configDialog, []string) error
}

func (d *configDialog) clear() {
	d.cancelOAuth()
	*d = configDialog{}
}

// takeSaved hands the owning Model the config written since the last call.
func (d *configDialog) takeSaved() *config.Config {
	c := d.saved
	d.saved = nil
	return c
}

func (d configDialog) isForm() bool {
	return d.active && d.view == configFormView
}

func (d configDialog) caps(id string) providerCaps {
	oauthOK := oauth.Supports(id)
	if p, ok := d.draft.Provider(id); ok {
		return providerCaps{custom: p.Custom, oauth: oauthOK && !p.Custom}
	}
	return providerCaps{oauth: oauthOK} // connecting from catalog
}

func (d configDialog) findPreset(id string) (config.Preset, bool) {
	for _, p := range d.presets {
		if p.ID == id {
			return p, true
		}
	}
	return config.Preset{}, false
}

// Open starts the dialog from a clone of the live config.
func (d *configDialog) Open(live config.Config) tea.Cmd {
	d.clear()
	d.active = true
	d.draft = live.Clone()
	return d.openPresets()
}

// Update handles dialog-owned messages. handled=false leaves the msg to Model.
func (d *configDialog) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !d.active {
		return nil, false
	}
	switch msg := msg.(type) {
	case modelsDevLoadedMsg:
		if msg.gen != d.loadGen {
			return nil, true
		}
		d.applyModelsDev(msg)
		return nil, true
	case oauthDeviceMsg:
		return d.handleOAuthDevice(msg), true
	case oauthDoneMsg:
		return d.handleOAuthDone(msg), true
	case tea.PasteMsg:
		return d.paste(msg), true
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return nil, false
		}
		return d.handleKey(msg), true
	default:
		if d.isForm() {
			return d.UpdateForm(msg), true
		}
		return nil, false
	}
}

// UpdateForm forwards residual msgs (e.g. cursor blink) to the focused field.
func (d *configDialog) UpdateForm(msg tea.Msg) tea.Cmd {
	if !d.isForm() || len(d.form.fields) == 0 {
		return nil
	}
	i := d.form.focus
	var cmd tea.Cmd
	d.form.fields[i], cmd = d.form.fields[i].Update(msg)
	return cmd
}

// View renders the centered dialog panel on a scrim (full terminal area).
func (d configDialog) View(chrome styles.Chrome, termW, areaW, areaH int) string {
	if !d.active {
		return ""
	}
	dlg := NewDialog(chrome)
	panel := d.renderPanel(chrome, termW, dlg)
	if panel == "" {
		return ""
	}
	return dlg.Place(areaW, areaH, panel)
}

func (d *configDialog) paste(msg tea.PasteMsg) tea.Cmd {
	if d.isForm() {
		return d.UpdateForm(msg)
	}
	if d.loading {
		return nil
	}
	switch d.view {
	case configPresets:
		d.setPresetQuery(d.presetQuery + msg.Content)
	case configModels:
		d.setModelQuery(d.modelQuery + msg.Content)
	}
	return nil
}

// mutate clones draft, applies fn, validates, saves, then swaps + parks the
// result in saved for the Model to collect.
func (d *configDialog) mutate(fn func(*config.Config) error) error {
	next := d.draft.Clone()
	if err := fn(&next); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if err := next.Save(); err != nil {
		return err
	}
	d.draft = next
	saved := next.Clone()
	d.saved = &saved
	return nil
}

func newFormInput(placeholder string, password bool) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 0
	ti.Prompt = ""
	ti.SetVirtualCursor(false)
	ti.KeyMap.AcceptSuggestion = key.NewBinding()
	ti.KeyMap.NextSuggestion = key.NewBinding()
	ti.KeyMap.PrevSuggestion = key.NewBinding()
	if password {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	return ti
}

// formFieldView renders a field value (and focused cursor) with panel BG.
func formFieldView(ti textinput.Model, width int, bg color.Color, focused bool) string {
	base := lipgloss.NewStyle()
	ph := styles.Placeholder
	if bg != nil {
		base = base.Background(bg)
		ph = ph.Background(bg)
	}
	val := ti.Value()
	style := base
	shown := val
	if val == "" {
		shown = ti.Placeholder
		style = ph
	} else if ti.EchoMode == textinput.EchoPassword {
		shown = strings.Repeat(string(ti.EchoCharacter), utf8.RuneCountInString(val))
	}
	if focused {
		shown += "█"
	}
	if lipgloss.Width(shown) > width {
		shown = truncateRight(shown, width)
	}
	pad := width - lipgloss.Width(shown)
	out := style.Render(shown)
	if pad > 0 {
		out += base.Render(strings.Repeat(" ", pad))
	}
	return out
}

func (d *configDialog) focusField(i int) {
	n := len(d.form.fields)
	if n == 0 {
		return
	}
	i %= n
	if i < 0 {
		i += n
	}
	for j := range d.form.fields {
		if j == i {
			d.form.fields[j].Focus()
		} else {
			d.form.fields[j].Blur()
		}
	}
	d.form.focus = i
}

func formValues(fields []textinput.Model) []string {
	out := make([]string, len(fields))
	for i := range fields {
		out[i] = fields[i].Value()
	}
	return out
}

type connectKind int

const (
	connectConfigured connectKind = iota
	connectCatalog                // available from models.dev, not yet connected
	connectCustom                 // "Custom endpoint" row
)

type connectRow struct {
	kind  connectKind
	id    string
	name  string
	tag   string // e.g. " (Custom)"
	count int    // enabled models (configured)
}

func (r connectRow) group() string {
	if r.kind == connectConfigured {
		return connectGroupConfigured
	}
	return connectGroupProviders
}

func presetHaystack(p config.Preset) string {
	return p.Name + " " + p.ID
}

func (d *configDialog) connectRows() []connectRow {
	q := d.presetQuery
	var connected, available []connectRow

	for _, id := range d.draft.ProviderIDs() {
		p := d.draft.Providers[id]
		hay := p.DisplayName(id) + " " + id
		if len(search.Indices(q, []string{hay})) == 0 {
			continue
		}
		tag := ""
		if p.Custom {
			tag = " (Custom)"
		} else if p.OAuth != nil {
			tag = " (OAuth)"
		}
		enabled := 0
		for _, md := range p.Models {
			if md.Enabled() {
				enabled++
			}
		}
		connected = append(connected, connectRow{
			kind:  connectConfigured,
			id:    id,
			name:  p.DisplayName(id),
			tag:   tag,
			count: enabled,
		})
	}

	for _, pre := range search.Filter(q, d.presets, presetHaystack) {
		if _, ok := d.draft.Provider(pre.ID); ok {
			continue
		}
		available = append(available, connectRow{
			kind: connectCatalog,
			id:   pre.ID,
			name: pre.Name,
		})
	}
	if len(search.Indices(q, []string{"custom endpoint"})) > 0 {
		available = append(available, connectRow{kind: connectCustom})
	}
	return append(connected, available...)
}

func (d *configDialog) setPresetQuery(q string) {
	d.presetQuery = q
	d.selected = 0
	d.status = ""
}

func (d *configDialog) setModelQuery(q string) {
	d.modelQuery = q
	d.selected = 0
	d.status = ""
}

func editQuery(q, key, text string) (string, bool) {
	switch key {
	case "backspace":
		if q == "" {
			return q, false
		}
		_, size := utf8.DecodeLastRuneInString(q)
		return q[:len(q)-size], true
	case "ctrl+u":
		if q == "" {
			return q, false
		}
		return "", true
	}
	if text != "" && key != "enter" {
		return q + text, true
	}
	return q, false
}

func renderConfigSearch(query, placeholder string, innerW int, bg color.Color) string {
	bar := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Dim).
		Padding(0, 1).
		Width(innerW)
	if bg != nil {
		bar = bar.Background(bg).BorderBackground(bg)
	}
	if query == "" {
		if placeholder == "" {
			placeholder = "Search…"
		}
		return bar.Foreground(styles.Dim).Italic(true).Render(placeholder)
	}
	return bar.Render(query)
}

type modelsDevLoadedMsg struct {
	gen     int
	presets []config.Preset
	err     error
}

func loadModelsDevCmd(gen int) tea.Cmd {
	return func() tea.Msg {
		raw, err := models.Load()
		return modelsDevLoadedMsg{gen: gen, presets: config.PresetsFromModels(raw), err: err}
	}
}

func (d *configDialog) openPresets() tea.Cmd {
	d.status = ""
	d.view = configPresets
	d.presetQuery = ""
	d.listSel.clear()
	d.form = configForm{}
	if len(d.presets) > 0 {
		d.loading = false
		return nil
	}
	d.loading = true
	d.loadGen++
	d.status = "loading providers…"
	return loadModelsDevCmd(d.loadGen)
}

func (d *configDialog) applyModelsDev(msg modelsDevLoadedMsg) {
	d.loading = false
	d.presets = msg.presets
	if msg.err != nil && len(msg.presets) > 0 {
		d.status = "offline · using cache"
		return
	}
	if msg.err != nil {
		d.status = msg.err.Error()
		return
	}
	d.status = ""
}
