package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/session"
	"github.com/axispx/zeta/internal/styles"
)

// pickerState is the full-screen session resume list.
type pickerState struct {
	active  bool
	entries []session.IndexEntry
	listSel
}

func (p *pickerState) clear() {
	p.active = false
	p.entries = nil
	p.listSel.clear()
}

func (m *Model) openPicker() {
	entries, err := session.List(m.ws.Abs)
	if err != nil {
		m.messages = append(m.messages, Message{Role: RoleError, Text: "session list: " + err.Error()})
		m.refreshTranscript()
		return
	}
	if len(entries) == 0 {
		m.messages = append(m.messages, Message{Role: RoleSystem, Text: "no sessions to resume"})
		m.refreshTranscript()
		return
	}
	m.picker.clear()
	m.picker.active = true
	m.picker.entries = entries
}

func (m *Model) resumeSelected() tea.Cmd {
	if !m.picker.active || len(m.picker.entries) == 0 {
		return nil
	}
	id := m.picker.entries[m.picker.selected].ID
	m.picker.clear()

	sess, recs, err := session.OpenID(m.ws.Abs, id)
	m.applySession(sess, recs, err)
	return m.ensureTitle(firstUserPrompt(m.messages))
}

func (m *Model) handlePickerKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if m.picker.move(len(m.picker.entries), key) {
		return nil
	}
	switch key {
	case "enter":
		return m.resumeSelected()
	case "esc":
		m.picker.clear()
		return nil
	}
	return nil
}

func formatPickerHeader(innerW int) string {
	prefix := strings.Repeat(" ", inputPromptWidth)
	return formatHintRow(prefix, "NAME", "UPDATED", innerW, styles.OverlayHeader, styles.OverlayHeader)
}

func (m Model) renderPicker(width, height int) string {
	if !m.picker.active || len(m.picker.entries) == 0 {
		return ""
	}

	innerW := width - 2*styles.ContentInset
	if innerW < 1 {
		innerW = 1
	}

	hintBar := styles.OverlayHintBar.
		Width(width).
		Padding(0, styles.ContentInset).
		Render("↑/↓ select · enter open · esc cancel")
	hintH := lipgloss.Height(hintBar)
	header := formatPickerHeader(innerW)
	headerH := lipgloss.Height(header)
	listH := height - hintH - headerH
	if listH < 1 {
		listH = 1
	}

	currentID := ""
	if m.sess != nil {
		currentID = m.sess.ID
	}

	start, end := windowAround(m.picker.selected, len(m.picker.entries), listH)
	var b strings.Builder
	b.WriteString(header)
	for i, e := range m.picker.entries[start:end] {
		b.WriteByte('\n')
		label := e.Name
		if label == "" {
			label = e.ID
		}
		sel := start+i == m.picker.selected
		b.WriteString(formatAccentRow(label, formatRelativeTime(e.Updated), innerW, sel, e.ID == currentID))
	}

	listBody := lipgloss.Place(width, listH+headerH, lipgloss.Left, lipgloss.Top, styles.Transcript.Render(b.String()))
	return lipgloss.JoinVertical(lipgloss.Left, listBody, hintBar)
}

func formatRelativeTime(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			return ""
		}
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}
