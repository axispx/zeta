package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
)

func (d *configDialog) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch d.view {
	case configFormView:
		return d.handleFormKey(msg)
	case configModels:
		return d.handleModelsKey(msg)
	case configAuth:
		return d.handleAuthKey(msg)
	default:
		return d.handlePresetsKey(msg)
	}
}

func (d *configDialog) handlePresetsKey(msg tea.KeyPressMsg) tea.Cmd {
	if d.loading {
		if msg.String() == "esc" {
			d.clear()
		}
		return nil
	}
	key := msg.String()
	items := d.connectRows()
	n := len(items)
	if d.move(n, key) {
		d.status = ""
		return nil
	}
	switch key {
	case "enter":
		d.status = ""
		if n == 0 || d.selected >= n {
			return nil
		}
		it := items[d.selected]
		switch it.kind {
		case connectCustom:
			d.openCustomForm()
		case connectConfigured:
			d.openModels(it.id)
		case connectCatalog:
			if d.caps(it.id).authChooser() {
				d.openAuthMethods(it.id)
			} else {
				d.openAPIKeyForm(it.id)
			}
		}
		return nil
	case "ctrl+x":
		d.status = ""
		if n == 0 || d.selected >= n {
			return nil
		}
		it := items[d.selected]
		if it.kind != connectConfigured {
			return nil
		}
		id := it.id
		if err := d.mutate(func(c *config.Config) error {
			return c.DeleteProvider(id)
		}); err != nil {
			d.status = err.Error()
			return nil
		}
		d.clamp(len(d.connectRows()))
		return nil
	case "ctrl+r":
		d.status = ""
		if n == 0 || d.selected >= n {
			return nil
		}
		it := items[d.selected]
		if it.kind != connectConfigured || !d.caps(it.id).canRename() {
			return nil
		}
		d.openRenameProvider(it.id)
		return nil
	case "esc":
		d.clear()
		return nil
	}
	if q, ok := editQuery(d.presetQuery, key, msg.Text); ok {
		d.setPresetQuery(q)
	}
	return nil
}

func (d *configDialog) handleModelsKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	rows := d.modelRows()
	n := len(rows)
	caps := d.caps(d.focusID)
	if d.move(n, key) {
		d.status = ""
		return nil
	}
	switch key {
	case "enter":
		d.status = ""
		if n == 0 || d.selected >= n {
			return nil
		}
		return d.activateModelRow(rows[d.selected])
	case "ctrl+k":
		d.status = ""
		d.openEditProvider()
		return nil
	case "ctrl+a":
		d.status = ""
		return d.toggleAllModels()
	case "ctrl+x":
		d.status = ""
		if !caps.canEditModels() || n == 0 || d.selected >= n {
			return nil
		}
		return d.removeCustomModel(rows[d.selected])
	case "ctrl+e":
		d.status = ""
		if !caps.canEditModels() || n == 0 || d.selected >= n {
			return nil
		}
		d.openEditModel(rows[d.selected])
		return nil
	case "esc":
		// Back one level, filter intact: the presets list is the screen the
		// user left, not a fresh one. ctrl+u clears a search box.
		d.status = ""
		d.view = configPresets
		d.listSel.clear()
		return nil
	}
	if q, ok := editQuery(d.modelQuery, key, msg.Text); ok {
		d.setModelQuery(q)
	}
	return nil
}

func (d *configDialog) handleFormKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	switch key {
	case "esc":
		d.cancelForm()
		return nil
	case "tab", "down":
		d.focusField(d.form.focus + 1)
		return nil
	case "shift+tab", "up":
		d.focusField(d.form.focus - 1)
		return nil
	case "enter":
		return d.submitForm()
	}
	return d.UpdateForm(msg)
}

func (d *configDialog) cancelForm() {
	back := d.form.back
	d.status = ""
	d.form = configForm{}
	d.modelID = ""
	d.listSel.clear()
	switch back {
	case backModels:
		d.enterModels()
	case backAuth:
		d.openAuthMethods(d.focusID)
	case backModelsIfConfigured:
		if _, ok := d.draft.Provider(d.focusID); ok {
			d.enterModels()
			return
		}
		d.view = configPresets
	default:
		d.view = configPresets
	}
}
