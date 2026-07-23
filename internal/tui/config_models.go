package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/search"
)

type modelRowKind int

const (
	modelRowToggle modelRowKind = iota
	modelRowAdd
)

type modelRow struct {
	kind modelRowKind
	id   string
	name string
	ctx  int
	on   bool
}

func (d *configDialog) openModels(providerID string) {
	d.status = ""
	d.focusID = providerID
	d.modelQuery = ""
	d.listSel.clear()
	// Sync catalog once on entry — draft then matches display; toggles just flip flags.
	if !d.caps(providerID).custom {
		if pre, ok := d.findPreset(providerID); ok {
			if err := d.mutate(func(c *config.Config) error {
				return c.SyncCatalogModels(providerID, pre.Models)
			}); err != nil {
				d.status = err.Error()
			}
		}
	}
	d.enterModels()
}

func (d *configDialog) enterModels() {
	d.view = configModels
	d.form = configForm{}
	d.modelID = ""
}

func (d *configDialog) providerModels(pid string) map[string]config.ModelDef {
	p, ok := d.draft.Providers[pid]
	if !ok {
		return nil
	}
	return p.Models
}

func (d *configDialog) modelRows() []modelRow {
	pid := d.focusID
	models := d.providerModels(pid)
	if models == nil {
		models = map[string]config.ModelDef{}
	}

	var toggles []modelRow
	for _, id := range (config.Provider{Models: models}).ModelIDs() {
		md := models[id]
		toggles = append(toggles, modelRow{
			kind: modelRowToggle,
			id:   id,
			name: md.DisplayName(id),
			ctx:  md.ContextWindow,
			on:   md.Enabled(),
		})
	}
	toggles = search.Filter(d.modelQuery, toggles, func(r modelRow) string {
		return r.name + " " + r.id
	})
	sort.SliceStable(toggles, func(i, j int) bool {
		if toggles[i].on != toggles[j].on {
			return toggles[i].on
		}
		return toggles[i].id < toggles[j].id
	})

	if d.caps(pid).canEditModels() {
		toggles = append(toggles, modelRow{kind: modelRowAdd})
	}
	return toggles
}

func (d *configDialog) toggleAllModels() tea.Cmd {
	if !d.caps(d.focusID).canToggleAll() {
		return nil
	}
	allOn := true
	for _, r := range d.modelRows() {
		if r.kind == modelRowToggle && !r.on {
			allOn = false
			break
		}
	}
	enable := !allOn
	pid := d.focusID
	if err := d.mutate(func(c *config.Config) error {
		return c.SetAllModelsEnabled(pid, enable)
	}); err != nil {
		d.status = err.Error()
		return nil
	}
	d.status = ""
	d.clamp(len(d.modelRows()))
	return nil
}

func (d *configDialog) activateModelRow(row modelRow) tea.Cmd {
	switch row.kind {
	case modelRowAdd:
		d.openAddModelForm()
		return nil
	case modelRowToggle:
		return d.toggleModel(row)
	}
	return nil
}

func (d *configDialog) toggleModel(row modelRow) tea.Cmd {
	pid := d.focusID
	enable := !row.on
	if err := d.mutate(func(c *config.Config) error {
		return c.SetModelEnabled(pid, row.id, enable)
	}); err != nil {
		d.status = err.Error()
		return nil
	}
	d.status = ""
	d.clamp(len(d.modelRows()))
	return nil
}

func (d *configDialog) removeCustomModel(row modelRow) tea.Cmd {
	if !d.caps(d.focusID).canEditModels() {
		return nil
	}
	if row.kind != modelRowToggle || row.id == "" {
		return nil
	}
	pid := d.focusID
	if err := d.mutate(func(c *config.Config) error {
		return c.DeleteModel(pid, row.id)
	}); err != nil {
		d.status = err.Error()
		return nil
	}
	d.status = "removed · " + row.id
	d.clamp(len(d.modelRows()))
	return nil
}

func apiKeyPlaceholder(configured bool) string {
	if configured {
		return "Update API Key"
	}
	return "Enter API Key"
}

func parseContextWindow(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("context window required")
	}
	ctx, err := strconv.Atoi(raw)
	if err != nil || ctx <= 0 {
		return 0, fmt.Errorf("context window must be a positive integer")
	}
	return ctx, nil
}

func (d *configDialog) startForm(title string, labels []string, fields []textinput.Model, back formBack, submit func(*configDialog, []string) error) {
	d.status = ""
	d.view = configFormView
	d.form = configForm{title: title, labels: labels, fields: fields, back: back, submit: submit}
	d.focusField(0)
}

func (d *configDialog) submitForm() tea.Cmd {
	if d.form.submit == nil {
		return nil
	}
	d.status = ""
	if err := d.form.submit(d, formValues(d.form.fields)); err != nil {
		d.status = err.Error()
	}
	return nil
}

func (d *configDialog) openAPIKeyForm(providerID string) {
	pre, ok := d.findPreset(providerID)
	if !ok {
		d.status = "unknown provider"
		return
	}
	d.focusID = providerID
	_, configured := d.draft.Provider(providerID)
	title := "Connect · " + pre.Name
	if configured {
		title = "API Key · " + pre.Name
	}
	back := backModelsIfConfigured
	if d.caps(providerID).authChooser() {
		back = backAuth
	}
	d.startForm(title, []string{"API Key"}, []textinput.Model{
		newFormInput(apiKeyPlaceholder(configured), true),
	}, back, submitAPIKey)
}

func submitAPIKey(d *configDialog, vals []string) error {
	key := strings.TrimSpace(vals[0])
	pre, ok := d.findPreset(d.focusID)
	if !ok {
		return fmt.Errorf("unknown provider")
	}
	if key == "" {
		if existing, ok := d.draft.Provider(d.focusID); ok && existing.HasUsableCredential() {
			d.listSel.clear()
			d.enterModels()
			return nil
		}
		return fmt.Errorf("api_key required")
	}
	if err := d.mutate(func(c *config.Config) error {
		return c.ConnectPreset(pre, key)
	}); err != nil {
		return err
	}
	d.status = "connected · " + d.focusID
	d.listSel.clear()
	d.enterModels()
	return nil
}

func (d *configDialog) openCustomForm() {
	d.focusID = ""
	d.startForm("Custom provider", []string{"Name", "ID", "Base URL", "API Key"}, []textinput.Model{
		newFormInput("OpenRouter", false),
		newFormInput("openrouter", false),
		newFormInput("https://openrouter.ai/api/v1", false),
		newFormInput(apiKeyPlaceholder(false), true),
	}, backPresets, submitCustom)
}

func submitCustom(d *configDialog, vals []string) error {
	name := strings.TrimSpace(vals[0])
	id := strings.TrimSpace(vals[1])
	base := strings.TrimSpace(vals[2])
	key := strings.TrimSpace(vals[3])
	if _, ok := d.draft.Provider(id); ok {
		return fmt.Errorf("%q is an existing provider id", id)
	}
	if _, ok := d.findPreset(id); ok {
		return fmt.Errorf("%q is in the catalog — pick it from the list", id)
	}
	if err := d.mutate(func(c *config.Config) error {
		return c.ConnectCustom(id, name, base, key)
	}); err != nil {
		return err
	}
	d.focusID = id
	d.status = "connected · " + id
	d.listSel.clear()
	d.enterModels()
	return nil
}

func (d *configDialog) openEditProvider() {
	p, ok := d.draft.Provider(d.focusID)
	if !ok {
		d.status = "provider not found"
		return
	}
	if d.caps(d.focusID).authChooser() {
		d.openAuthMethods(d.focusID)
		return
	}
	if d.caps(d.focusID).apiKeyOnly() {
		d.openAPIKeyForm(d.focusID)
		return
	}
	base := newFormInput("https://…", false)
	base.SetValue(p.BaseURL)
	d.startForm("Edit · "+d.focusID, []string{"Base URL", "API Key"}, []textinput.Model{
		base,
		newFormInput(apiKeyPlaceholder(true), true),
	}, backModels, submitEditProvider)
}

func submitEditProvider(d *configDialog, vals []string) error {
	pid := d.focusID
	if _, ok := d.draft.Provider(pid); !ok {
		return fmt.Errorf("provider not found")
	}
	base := strings.TrimSpace(vals[0])
	key := strings.TrimSpace(vals[1])
	if err := d.mutate(func(c *config.Config) error {
		return c.UpdateProvider(pid, "", base, key)
	}); err != nil {
		return err
	}
	d.status = "saved · " + pid
	d.enterModels()
	return nil
}

func (d *configDialog) openAddModelForm() {
	d.startForm("Add model · "+d.focusID, []string{"Model", "Context"}, []textinput.Model{
		newFormInput("model-id", false),
		newFormInput("128000", false),
	}, backModels, submitAddModel)
}

func submitAddModel(d *configDialog, vals []string) error {
	modelID := strings.TrimSpace(vals[0])
	if modelID == "" {
		return fmt.Errorf("model id required")
	}
	ctx, err := parseContextWindow(vals[1])
	if err != nil {
		return err
	}
	pid := d.focusID
	if p, ok := d.draft.Provider(pid); ok {
		if _, exists := p.Models[modelID]; exists {
			return fmt.Errorf("model %q already exists", modelID)
		}
	}
	if err := d.mutate(func(c *config.Config) error {
		return c.UpsertModel(pid, modelID, config.ModelDef{ContextWindow: ctx})
	}); err != nil {
		return err
	}
	d.status = "added · " + modelID
	d.listSel.clear()
	d.enterModels()
	return nil
}

func (d *configDialog) openRenameProvider(providerID string) {
	p, ok := d.draft.Provider(providerID)
	if !ok || !d.caps(providerID).canRename() {
		return
	}
	d.focusID = providerID
	name := newFormInput("display name", false)
	name.SetValue(p.DisplayName(providerID))
	d.startForm("Rename · "+providerID, []string{"Name"}, []textinput.Model{name}, backPresets, submitRenameProvider)
}

func submitRenameProvider(d *configDialog, vals []string) error {
	name := strings.TrimSpace(vals[0])
	if name == "" {
		return fmt.Errorf("name required")
	}
	pid := d.focusID
	if !d.caps(pid).canRename() {
		return fmt.Errorf("catalog providers can't be renamed")
	}
	if err := d.mutate(func(c *config.Config) error {
		return c.UpdateProvider(pid, name, "", "")
	}); err != nil {
		return err
	}
	d.status = "renamed · " + name
	d.form = configForm{}
	d.view = configPresets
	return nil
}

func (d *configDialog) openEditModel(row modelRow) {
	if !d.caps(d.focusID).canEditModels() {
		return
	}
	if row.kind != modelRowToggle || row.id == "" {
		return
	}
	p, ok := d.draft.Provider(d.focusID)
	if !ok {
		d.status = "provider not found"
		return
	}
	md, ok := p.Models[row.id]
	if !ok {
		d.status = "model not found"
		return
	}
	d.modelID = row.id
	name := newFormInput("model-id", false)
	name.SetValue(md.DisplayName(row.id))
	ctx := newFormInput("128000", false)
	if md.ContextWindow > 0 {
		ctx.SetValue(strconv.Itoa(md.ContextWindow))
	}
	d.startForm("Edit model · "+row.id, []string{"Model", "Context"}, []textinput.Model{name, ctx}, backModels, submitEditModel)
}

func submitEditModel(d *configDialog, vals []string) error {
	name := strings.TrimSpace(vals[0])
	if name == "" {
		return fmt.Errorf("model required")
	}
	ctx, err := parseContextWindow(vals[1])
	if err != nil {
		return err
	}
	pid := d.focusID
	mid := d.modelID
	if !d.caps(pid).canEditModels() {
		return fmt.Errorf("catalog models can't be edited")
	}
	p, ok := d.draft.Provider(pid)
	if !ok {
		return fmt.Errorf("provider not found")
	}
	md, ok := p.Models[mid]
	if !ok {
		return fmt.Errorf("model not found")
	}
	md.Name = name
	md.ContextWindow = ctx
	if err := d.mutate(func(c *config.Config) error {
		return c.UpsertModel(pid, mid, md)
	}); err != nil {
		return err
	}
	d.status = "saved · " + mid
	d.enterModels()
	return nil
}
