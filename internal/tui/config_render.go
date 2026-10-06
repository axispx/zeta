package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
)

func (d configDialog) renderPanel(chrome styles.Chrome, termW int, dlg Dialog) string {
	if !d.active {
		return ""
	}
	ink := chrome.OverlayInk()
	panelW, contentW := dlg.FitWidth(termW)

	var body string
	var footer DialogFooter
	switch d.view {
	case configFormView:
		body, footer = d.formBody(contentW, chrome, ink)
	case configModels:
		body, footer = d.modelsBody(contentW, chrome, ink)
	case configAuth:
		body, footer = d.authBody(contentW, chrome, ink)
	case configReview:
		body, footer = d.reviewBody(contentW, ink)
	default:
		if d.tab == tabSettings {
			body, footer = d.settingsBody(contentW, ink)
		} else {
			body, footer = d.presetsBody(contentW, chrome, ink)
		}
	}
	if d.status != "" {
		body += "\n" + styles.SystemMsg.Render(d.status)
	}
	return dlg.PanelWithFooter(body, footer, panelW, ink)
}

func configEscTitle(title string, innerW int, ink styles.OverlayInk) string {
	return formatHintRow("", title, "esc", innerW, ink.Header, ink.Hint, ink.Gap)
}

// tabsFooter is the presets view's footer: the row hints, then the tab switch.
func tabsFooter(hints []string, ink styles.OverlayInk) DialogFooter {
	hints = append(hints, ink.HintKbd("Switch tabs", "shift+tab"))
	return DialogFooter{Hint: strings.Join(hints, ink.Gap.Render("  "))}
}

// tabsTitle is the presets view's title row: the tab strip with the active tab
// highlighted, and the esc hint on the right.
func (d configDialog) tabsTitle(innerW int, ink styles.OverlayInk) string {
	var tabs []string
	for i, name := range configTabNames {
		if configTab(i) == d.tab {
			tabs = append(tabs, ink.Selected.Bold(true).Render(name))
		} else {
			tabs = append(tabs, ink.Hint.Render(name))
		}
	}
	left := strings.Join(tabs, ink.Gap.Render("  "))
	hint := ink.Hint.Render("esc")
	pad := innerW - lipgloss.Width(left) - lipgloss.Width(hint)
	if pad < 1 {
		pad = 1
	}
	return left + ink.Gap.Render(strings.Repeat(" ", pad)) + hint
}

func (d configDialog) settingsBody(innerW int, ink styles.OverlayInk) (body string, footer DialogFooter) {
	rows := settingRows()
	var hints []string
	if d.selected < len(rows) && rows[d.selected].key != nil {
		hints = append(hints, ink.HintKbd(rows[d.selected].keyLabel, "ctrl+k"))
	}
	footer = tabsFooter(hints, ink)

	var b strings.Builder
	b.WriteString(d.tabsTitle(innerW, ink))
	b.WriteByte('\n')
	b.WriteString(ink.Hint.Render("Enter to toggle"))
	b.WriteByte('\n')
	for i, row := range rows {
		b.WriteByte('\n')
		b.WriteString(formatAccentRow(row.name, row.value(d), innerW, i == d.selected, false, ink))
	}
	return b.String(), footer
}

func (d configDialog) presetsBody(innerW int, chrome styles.Chrome, ink styles.OverlayInk) (body string, footer DialogFooter) {
	items := d.connectRows()
	n := len(items)
	var hints []string
	if n > 0 && d.selected < n {
		it := items[d.selected]
		if it.kind == connectConfigured {
			hints = append(hints, ink.HintKbd("Remove", "ctrl+x"))
			if d.caps(it.id).canRename() {
				hints = append(hints, ink.HintKbd("Rename", "ctrl+r"))
			}
		}
	}
	footer = tabsFooter(hints, ink)
	title := d.tabsTitle(innerW, ink)

	if d.loading {
		var b strings.Builder
		b.WriteString(title)
		b.WriteString("\n\n")
		b.WriteString(ink.Hint.Render("loading from models.dev…"))
		return b.String(), tabsFooter(nil, ink)
	}

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(renderConfigSearch(d.presetQuery, "Search providers…", innerW, chrome.Input))

	if n == 0 {
		b.WriteString("\n\n")
		b.WriteString(ink.Hint.Render(strings.Repeat(" ", inputPromptWidth) + "no matches"))
		return b.String(), footer
	}

	listH := configDialogListRows
	if n < listH {
		listH = n
	}
	start, end := windowAround(d.selected, n, listH)

	for i := start; i < end; i++ {
		it := items[i]
		if i == start || it.group() != items[i-1].group() {
			b.WriteByte('\n')
			label := "Providers"
			if it.group() == connectGroupConfigured {
				label = "Configured"
			}
			b.WriteByte('\n')
			section := lipgloss.NewStyle().Foreground(styles.Blue)
			if chrome.Input != nil {
				section = section.Background(chrome.Input)
			}
			b.WriteString(section.Render(strings.Repeat(" ", inputPromptWidth) + label))
		}
		b.WriteByte('\n')
		sel := i == d.selected
		switch it.kind {
		case connectCustom:
			b.WriteString(formatAccentRow("Custom", "endpoint", innerW, sel, false, ink))
		case connectConfigured:
			b.WriteString(formatAccentRowTagged(it.name, it.tag, strconv.Itoa(it.count), innerW, sel, false, ink))
		case connectCatalog:
			b.WriteString(formatAccentRow(it.name, "", innerW, sel, false, ink))
		}
	}
	return b.String(), footer
}

func (d configDialog) modelsBody(innerW int, chrome styles.Chrome, ink styles.OverlayInk) (body string, footer DialogFooter) {
	caps := d.caps(d.focusID)
	var hints []string
	credLabel := "Update API Key"
	if d.caps(d.focusID).authChooser() {
		credLabel = "Credentials"
	}
	hints = append(hints, ink.HintKbd(credLabel, "ctrl+k"))
	if caps.canToggleAll() {
		hints = append(hints, ink.HintKbd("Toggle All", "ctrl+a"))
	} else if caps.canEditModels() {
		rows := d.modelRows()
		if n := len(rows); n > 0 && d.selected < n && rows[d.selected].kind == modelRowToggle {
			hints = append(hints, ink.HintKbd("Remove", "ctrl+x"))
			hints = append(hints, ink.HintKbd("Edit", "ctrl+e"))
		}
	}
	footer = DialogFooter{Hint: strings.Join(hints, ink.Gap.Render("  "))}
	titleName := d.focusID
	if p, ok := d.draft.Provider(d.focusID); ok {
		titleName = p.DisplayName(d.focusID)
	} else if pre, ok := d.findPreset(d.focusID); ok {
		titleName = pre.Name
	}

	var b strings.Builder
	b.WriteString(configEscTitle(titleName, innerW, ink))
	b.WriteByte('\n')
	b.WriteString(ink.Hint.Render("Enter to toggle"))
	b.WriteString("\n\n")
	b.WriteString(renderConfigSearch(d.modelQuery, "Search models…", innerW, chrome.Input))

	rows := d.modelRows()
	n := len(rows)
	listH := configDialogListRows
	if n < listH {
		listH = n
	}
	if listH < 1 {
		listH = 1
	}
	start, end := windowAround(d.selected, n, listH)

	if n == 0 {
		b.WriteString("\n\n")
		b.WriteString(ink.Hint.Render(strings.Repeat(" ", inputPromptWidth) + "no models"))
		return b.String(), footer
	}
	for i := start; i < end; i++ {
		b.WriteByte('\n')
		if i == start {
			b.WriteByte('\n')
		}
		row := rows[i]
		sel := i == d.selected
		switch row.kind {
		case modelRowAdd:
			b.WriteString(formatAccentRow("+ Add model", "", innerW, sel, false, ink))
		default:
			mark := "○ "
			if row.on {
				mark = "● "
			}
			right := ""
			if row.ctx > 0 {
				right = strconv.Itoa(row.ctx)
			}
			b.WriteString(formatAccentRow(mark+row.name, right, innerW, sel, false, ink))
		}
	}
	return b.String(), footer
}

// reviewBody is the auto review backend chooser shown when review is turned on.
func (d configDialog) reviewBody(innerW int, ink styles.OverlayInk) (body string, footer DialogFooter) {
	var b strings.Builder
	b.WriteString(configEscTitle("Auto review", innerW, ink))
	b.WriteByte('\n')
	b.WriteString(ink.Hint.Render("Send commands to:"))
	rows := reviewBackendRows()
	for i, row := range rows {
		b.WriteByte('\n')
		if i == 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatAccentRow(row.name, row.hint, innerW, i == d.selected, false, ink))
	}
	return b.String(), DialogFooter{}
}

func (d configDialog) authBody(innerW int, chrome styles.Chrome, ink styles.OverlayInk) (body string, footer DialogFooter) {
	title := d.authTitle
	if title == "" {
		title = "Credentials"
	}
	var b strings.Builder
	b.WriteString(configEscTitle(title, innerW, ink))
	b.WriteByte('\n')
	if d.oauth != nil {
		// A device flow shows the code the user has to type; a browser flow
		// just waits for the callback, so the code block stays hidden.
		footer = DialogFooter{Hint: ink.HintKbd("Cancel", "esc")}
		if d.oauth.userCode != "" {
			b.WriteString(ink.Hint.Render("Waiting for device authorization…"))
		} else {
			b.WriteString(ink.Hint.Render("Waiting for you to finish signing in…"))
		}
		if d.oauth.verifyURL != "" {
			b.WriteString("\n\n")
			link := lipgloss.NewStyle().
				Foreground(ink.Selected.GetForeground()).
				Underline(true).
				Hyperlink(d.oauth.verifyURL).
				Render(d.oauth.verifyURL)
			b.WriteString(link)
		}
		if d.oauth.userCode != "" {
			b.WriteString("\n\n")
			b.WriteString(ink.Hint.Render("Code  "))
			b.WriteString(ink.Selected.Bold(true).Render(d.oauth.userCode))
		}
		return b.String(), footer
	}
	b.WriteString(ink.Hint.Render("Choose how to authenticate"))
	rows := authMethodRows(d.focusID)
	for i, row := range rows {
		b.WriteByte('\n')
		if i == 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatAccentRow(row.name, row.hint, innerW, i == d.selected, false, ink))
	}
	return b.String(), footer
}

func (d configDialog) formBody(innerW int, chrome styles.Chrome, ink styles.OverlayInk) (body string, footer DialogFooter) {
	if len(d.form.fields) > 1 {
		footer = DialogFooter{Hint: strings.Join([]string{
			ink.HintKbd("Next", "tab"),
			ink.HintKbd("Save", "enter"),
		}, ink.Gap.Render("  "))}
	}

	title := d.form.title
	if title == "" {
		title = "Form"
	}

	var b strings.Builder
	b.WriteString(configEscTitle(title, innerW, ink))
	b.WriteByte('\n')

	labelW := 0
	for _, l := range d.form.labels {
		if w := lipgloss.Width(l); w > labelW {
			labelW = w
		}
	}
	inputW := innerW - labelW - 2
	if inputW < 10 {
		inputW = 10
	}

	for i, label := range d.form.labels {
		b.WriteByte('\n')
		field := d.form.fields[i]
		focused := i == d.form.focus
		labelStyle := ink.Hint
		if focused {
			labelStyle = ink.Selected
		}
		val := formFieldView(field, inputW, chrome.Input, focused)
		b.WriteString(labelStyle.Width(labelW).Render(label))
		b.WriteString(ink.Gap.Render("  "))
		b.WriteString(val)
	}
	return b.String(), footer
}
