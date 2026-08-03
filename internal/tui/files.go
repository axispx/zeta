package tui

import (
	"context"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/rg"
	"github.com/axispx/zeta/internal/search"
	"github.com/axispx/zeta/internal/styles"
)

const fileOverlayMaxRows = 5

// filePicker is @ mention inventory + filter data (selection lives on filterOverlay).
type filePicker struct {
	matches []string // filtered view (≤ fileOverlayMaxRows)
	all     []string // workspace inventory (nil until listed)
	query   string
	seq     int // monotonic; stale list msgs stay stale
	loading bool
	err     string
	cancel  context.CancelFunc // aborts in-flight ListFiles
}

// clear resets picker UI state and cancels any in-flight list.
// seq is monotonic across opens.
func (f *filePicker) clear() {
	if f.cancel != nil {
		f.cancel()
	}
	seq := f.seq
	*f = filePicker{seq: seq}
}

// visible reports rows (or a status line) the list can show.
// Empty matches after inventory loads → hidden; inventory stays for refilter.
func (f filePicker) visible() bool {
	if f.err != "" {
		return true
	}
	if f.loading && f.all == nil {
		return true // "searching…"
	}
	return len(f.matches) > 0
}

// atToken is an @path fragment under the cursor.
type atToken struct {
	start, end int // byte range in value covering @ + query
	query      string
}

// atTokenAtCursor finds a whitespace-delimited @token containing the cursor.
// Emails like user@host are rejected (token must start with @).
func atTokenAtCursor(val string, line, col int) (atToken, bool) {
	if val == "" {
		return atToken{}, false
	}
	off, ok := cursorByteOffset(val, line, col)
	if !ok {
		return atToken{}, false
	}
	// Token runs on one line only (newlines are delimiters).
	lineStart := strings.LastIndexByte(val[:off], '\n') + 1
	lineEnd := strings.IndexByte(val[off:], '\n')
	if lineEnd < 0 {
		lineEnd = len(val)
	} else {
		lineEnd = off + lineEnd
	}
	lineText := val[lineStart:lineEnd]
	rel := off - lineStart
	if rel < 0 || rel > len(lineText) {
		return atToken{}, false
	}

	// Walk left from cursor to token start (whitespace boundary).
	startRel := rel
	for startRel > 0 {
		r, size := utf8.DecodeLastRuneInString(lineText[:startRel])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if unicode.IsSpace(r) {
			break
		}
		startRel -= size
	}
	// Walk right from cursor to token end.
	endRel := rel
	for endRel < len(lineText) {
		r, size := utf8.DecodeRuneInString(lineText[endRel:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if unicode.IsSpace(r) {
			break
		}
		endRel += size
	}
	token := lineText[startRel:endRel]
	if !strings.HasPrefix(token, "@") {
		return atToken{}, false
	}
	// Query may only use path-ish characters; stop early if junk appears.
	query := token[1:]
	for i, r := range query {
		if !isAtPathRune(r) {
			// Cursor past the valid query portion → no active mention.
			validEnd := 1 + i // relative to token, includes @
			if rel > startRel+validEnd {
				return atToken{}, false
			}
			query = query[:i]
			endRel = startRel + 1 + len(query)
			break
		}
	}
	return atToken{
		start: lineStart + startRel,
		end:   lineStart + endRel,
		query: query,
	}, true
}

func isAtPathRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	switch r {
	case '_', '-', '.', '/', '~', '+':
		return true
	}
	return false
}

// cursorByteOffset maps textarea (line, col) to a byte offset in val.
// col is a rune index on the line (bubbles textarea semantics).
func cursorByteOffset(val string, line, col int) (int, bool) {
	if line < 0 || col < 0 {
		return 0, false
	}
	lines := strings.Split(val, "\n")
	if line >= len(lines) {
		return 0, false
	}
	off := 0
	for i := 0; i < line; i++ {
		off += len(lines[i]) + 1 // + newline
	}
	runes := []rune(lines[line])
	if col > len(runes) {
		col = len(runes)
	}
	off += len(string(runes[:col]))
	if off > len(val) {
		off = len(val)
	}
	return off, true
}

// lineColAtByte is the inverse of cursorByteOffset.
func lineColAtByte(val string, off int) (line, col int) {
	if off < 0 {
		off = 0
	}
	if off > len(val) {
		off = len(val)
	}
	prefix := val[:off]
	line = strings.Count(prefix, "\n")
	lastNL := strings.LastIndexByte(prefix, '\n')
	seg := prefix
	if lastNL >= 0 {
		seg = prefix[lastNL+1:]
	}
	col = utf8.RuneCountInString(seg)
	return line, col
}

// fileListMsg is the async workspace inventory for the @ picker.
type fileListMsg struct {
	seq   int
	paths []string
	err   string
}

// ensureFileList loads the workspace file inventory once per overlay open.
// Query filtering is sync against all after the list arrives.
func (m *Model) ensureFileList() tea.Cmd {
	f := &m.overlay.files
	if f.loading || f.all != nil {
		return nil
	}
	root := m.ws.Abs
	// Cancel any leftover list (should already be gone after clear).
	if f.cancel != nil {
		f.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	f.loading = true
	f.err = ""
	f.seq++
	seq := f.seq
	return func() tea.Msg {
		paths, err := rg.ListFiles(ctx, root, 0)
		if err != nil {
			if ctx.Err() != nil {
				// Canceled: leave paths nil so applyFileListMsg ignores the msg.
				return fileListMsg{seq: seq}
			}
			return fileListMsg{seq: seq, err: err.Error()}
		}
		// ListFiles returns non-nil paths (empty tree → []string{}).
		return fileListMsg{seq: seq, paths: paths}
	}
}

func (m *Model) applyFileListMsg(msg fileListMsg) {
	if m.overlay.mode != overlayFiles {
		return
	}
	f := &m.overlay.files
	if msg.seq != f.seq {
		return
	}
	f.loading = false
	f.cancel = nil
	// Canceled list: nil paths + empty err. Empty tree uses a non-nil empty slice.
	if msg.err == "" && msg.paths == nil {
		return
	}
	if msg.err != "" {
		f.err = msg.err
		f.all = nil
		f.matches = nil
		m.overlay.clamp(0)
		return
	}
	f.err = ""
	f.all = msg.paths
	m.refilterFiles()
}

// fileHaystack puts basename first so fuzzy prefers leaf names over deep paths.
func fileHaystack(p string) string { return path.Base(p) + " " + p }

// refilterFiles applies the current query against all (sync, top-K only).
func (m *Model) refilterFiles() {
	f := &m.overlay.files
	if f.all == nil {
		f.matches = nil
		m.overlay.clamp(0)
		return
	}
	matched := search.FilterN(f.query, f.all, fileOverlayMaxRows, fileHaystack)
	if len(matched) == 0 {
		f.matches = nil
	} else {
		f.matches = matched
	}
	m.overlay.clamp(len(f.matches))
}

func (m *Model) insertFileMention() {
	f := &m.overlay.files
	if m.overlay.mode != overlayFiles || len(f.matches) == 0 {
		return
	}
	path := f.matches[m.overlay.selected]
	if path == "" {
		return
	}
	val := m.textarea.Value()
	tok, ok := atTokenAtCursor(val, m.textarea.Line(), m.textarea.Column())
	if !ok {
		m.closeOverlay()
		return
	}
	insert := "@" + path + " "
	newVal := val[:tok.start] + insert + val[tok.end:]
	cursorAt := tok.start + len(insert)
	line, col := lineColAtByte(newVal, cursorAt)

	prevH := m.textarea.Height()
	m.textarea.SetValue(newVal)
	// SetValue leaves cursor at end; walk to the insert point (no byte-offset API).
	m.textarea.MoveToBegin()
	for i := 0; i < line; i++ {
		m.textarea.CursorDown()
	}
	m.textarea.SetCursorColumn(col)
	m.syncTextareaStyles()
	m.closeOverlay()
	if m.ready && m.textarea.Height() != prevH {
		m.layoutPreservingBottom()
	}
}

func (m Model) renderFileOverlay(width int) string {
	if m.overlay.mode != overlayFiles || !m.overlay.files.visible() {
		return ""
	}
	f := m.overlay.files
	innerW, contentW := overlayWidths(width)
	ink := m.chrome.OverlayInk()

	var body string
	switch {
	case f.err != "":
		body = formatFileStatusRow(f.err, contentW, ink)
	case f.loading && f.all == nil:
		body = formatFileStatusRow("searching…", contentW, ink)
	default:
		var b strings.Builder
		for i, p := range f.matches {
			if i > 0 {
				b.WriteByte('\n')
			}
			row := formatFileRow(p, i == m.overlay.selected, contentW, ink)
			b.WriteString(row)
		}
		body = b.String()
	}
	return m.paintOverlay(body, innerW)
}

func formatFileStatusRow(text string, contentW int, ink styles.OverlayInk) string {
	prefix := strings.Repeat(" ", inputPromptWidth)
	row := ink.Hint.Render(prefix + text)
	if contentW > 0 {
		row = ink.Gap.Width(contentW).Render(row)
	}
	return row
}

func formatFileRow(path string, selected bool, contentW int, ink styles.OverlayInk) string {
	prefix := strings.Repeat(" ", inputPromptWidth)
	labelStyle := ink.Row
	if selected {
		prefix = inputPrompt
		labelStyle = ink.Selected
	}
	label := path
	// Keep room for the prompt gutter inside contentW.
	maxW := contentW - inputPromptWidth
	if maxW < 1 {
		maxW = 1
	}
	if lipgloss.Width(label) > maxW {
		label = truncateLeft(label, maxW)
	}
	row := labelStyle.Render(prefix + label)
	if contentW > 0 {
		row = ink.Gap.Width(contentW).Render(row)
	}
	return row
}
