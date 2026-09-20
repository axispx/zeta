package tui

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/session"
)

// imageTokenRE matches inline image markers in the composer.
var imageTokenRE = regexp.MustCompile(`\[Image (\d+)\]`)

func imageToken(n int) string {
	return fmt.Sprintf("[Image %d]", n)
}

func transcriptLabel(r image.Ref, n int) string {
	name := r.Name
	if name == "" {
		name = "image"
	}
	return fmt.Sprintf("[Image %d · %s]", n, name)
}

// tryAttachPath normalizes pasted text as a single local image path and
// embeds it as a data: URL (nothing kept under ZETA_HOME).
func tryAttachPath(pasted string) (image.Ref, bool) {
	raw, ok := image.NormalizePath(pasted)
	if !ok {
		return image.Ref{}, false
	}
	abs, err := image.Abs(raw)
	if err != nil {
		return image.Ref{}, false
	}
	ref, err := image.RefFromPath(abs)
	if err != nil {
		return image.Ref{}, false
	}
	return ref, true
}

func cloneImages(src map[int]image.Ref) map[int]image.Ref {
	if len(src) == 0 {
		return nil
	}
	out := make(map[int]image.Ref, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (m *Model) clearPendingImages() {
	m.composer.pendingImages = nil
	m.composer.nextImageN = 0
}

// snapshotComposer captures textarea + image slots (live draft stash for ↑/↓).
func (m *Model) snapshotComposer() composerDraft {
	return composerDraft{
		Text:   m.composer.textarea.Value(),
		Images: cloneImages(m.composer.pendingImages),
		NextN:  m.composer.nextImageN,
	}
}

// applyComposer restores a stashed draft into the composer.
func (m *Model) applyComposer(d composerDraft) {
	m.composer.pendingImages = cloneImages(d.Images)
	m.composer.nextImageN = d.NextN
	m.setPromptValue(d.Text)
}

// insertImageAttach stores the image under a stable token id and inserts [Image N].
// Token numbers are never renumbered — deleting a token only drops that slot.
func (m *Model) insertImageAttach(ref image.Ref) {
	if m.composer.pendingImages == nil {
		m.composer.pendingImages = make(map[int]image.Ref)
	}
	m.composer.nextImageN++
	n := m.composer.nextImageN
	m.composer.pendingImages[n] = ref
	tok := imageToken(n)
	if m.needsSpaceBeforeInsert() {
		tok = " " + tok
	}
	// Trailing space so the next typed word doesn't stick to the token.
	tok += " "
	m.composer.textarea.InsertString(tok)
	m.syncTextareaStyles()
}

// needsSpaceBeforeInsert reports whether the cursor follows non-blank text, so
// an inserted token needs a leading space.
func (m *Model) needsSpaceBeforeInsert() bool {
	val := m.composer.textarea.Value()
	if val == "" {
		return false
	}
	lines := strings.Split(val, "\n")
	row := m.composer.textarea.Line()
	col := m.composer.textarea.Column()
	if row < 0 || row >= len(lines) {
		return false
	}
	line := lines[row]
	if col <= 0 {
		return false
	}
	if col > len(line) {
		col = len(line)
	}
	prev := line[col-1]
	return prev != ' ' && prev != '\t'
}

// tokenHit is one [Image N] span in composer text.
type tokenHit struct {
	start, end, n int
}

// findImageTokens returns [Image N] hits with 1 <= N <= maxN, in document order.
func findImageTokens(val string, maxN int) []tokenHit {
	if maxN < 1 {
		return nil
	}
	var hits []tokenHit
	for _, loc := range imageTokenRE.FindAllStringSubmatchIndex(val, -1) {
		n, err := strconv.Atoi(val[loc[2]:loc[3]])
		if err != nil || n < 1 || n > maxN {
			continue
		}
		hits = append(hits, tokenHit{start: loc[0], end: loc[1], n: n})
	}
	return hits
}

// syncPendingImages drops attachments whose [Image N] token was deleted.
// Token numbers stay stable (no textarea rewrite / renumber).
func (m *Model) syncPendingImages() {
	if len(m.composer.pendingImages) == 0 {
		return
	}
	val := m.composer.textarea.Value()
	live := make(map[int]bool, len(m.composer.pendingImages))
	for _, h := range findImageTokens(val, m.composer.nextImageN) {
		if _, ok := m.composer.pendingImages[h.n]; ok {
			live[h.n] = true
		}
	}
	for n := range m.composer.pendingImages {
		if !live[n] {
			delete(m.composer.pendingImages, n)
		}
	}
	if len(m.composer.pendingImages) == 0 {
		m.clearPendingImages()
	}
}

// parseComposer splits textarea content into plain text + images referenced by tokens.
// Token markers for live pending slots are stripped. Unknown/orphan tokens stay as text.
func (m *Model) parseComposer() (text string, imgs []image.Ref) {
	val := m.composer.textarea.Value()
	if len(m.composer.pendingImages) == 0 {
		return strings.TrimSpace(val), nil
	}
	hits := findImageTokens(val, m.composer.nextImageN)
	if len(hits) == 0 {
		return strings.TrimSpace(val), nil
	}
	seen := map[int]bool{}
	var b strings.Builder
	last := 0
	for _, h := range hits {
		ref, ok := m.composer.pendingImages[h.n]
		if !ok {
			continue // leave unknown token in text
		}
		b.WriteString(val[last:h.start])
		last = h.end
		if seen[h.n] {
			continue
		}
		seen[h.n] = true
		imgs = append(imgs, ref)
	}
	b.WriteString(val[last:])
	return collapseTokenGaps(b.String()), imgs
}

func collapseTokenGaps(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if prevSpace {
				continue
			}
			prevSpace = true
			b.WriteByte(' ')
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	out := b.String()
	out = strings.ReplaceAll(out, " \n", "\n")
	out = strings.ReplaceAll(out, "\n ", "\n")
	return strings.TrimSpace(out)
}

// userDisplayText builds transcript text for a user turn with optional images.
func userDisplayText(text string, imgs []image.Ref) string {
	var b strings.Builder
	b.WriteString(text)
	for i, img := range imgs {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(transcriptLabel(img, i+1))
	}
	return b.String()
}

func userDisplayFromSession(text string, refs []session.ImageRef) string {
	return userDisplayText(text, refs)
}

func (m *Model) afterComposerChange() {
	m.syncTextareaStyles()
	if m.term.ready {
		m.layoutPreservingBottom()
	}
}

// isPasteKey reports clipboard-paste chords. macOS terminals use super+v (⌘V);
// ctrl+v is the bubbles default and common on Linux.
func isPasteKey(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "ctrl+v", "super+v", "ctrl+alt+v", "ctrl+super+v":
		return true
	default:
		return false
	}
}

// handleBracketPaste handles tea.PasteMsg: attach if content is an image path,
// or insert dropped file paths, otherwise return false so the textarea receives
// the paste.
func (m *Model) handleBracketPaste(content string) bool {
	ref, ok := tryAttachPath(content)
	if !ok {
		return m.handleDropPaste(content)
	}
	m.insertImageAttach(ref)
	m.afterComposerChange()
	return true
}

// handleClipboardPaste handles ⌘V / ctrl+v: clipboard image first, else text
// (path attach or plain insert). Always consumes the key chord.
func (m *Model) handleClipboardPaste() {
	ref, err := image.ReadClipboardImage()
	if err == nil {
		m.insertImageAttach(ref)
		m.afterComposerChange()
		return
	}
	// Soft-miss (no image): fall through to text. Hard errors: status row.
	if !errors.Is(err, image.ErrNoImage) {
		m.transcript.messages = append(m.transcript.messages, Message{
			Role: RoleSystem,
			Text: "clipboard image: " + err.Error(),
		})
		m.refreshTranscript()
		return
	}

	text, rerr := clipboard.ReadAll()
	if rerr != nil || text == "" {
		return // consume chord; nothing to paste
	}
	if ref, ok := tryAttachPath(text); ok {
		m.insertImageAttach(ref)
		m.afterComposerChange()
		return
	}
	if m.handleDropPaste(text) {
		return
	}
	before := m.composer.textarea.Value()
	m.composer.textarea.InsertString(text)
	m.notePromptEdit(before)
	if m.composer.textarea.Value() != before {
		m.syncPendingImages()
	}
	m.afterComposerChange()
}
