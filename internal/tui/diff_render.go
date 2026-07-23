package tui

import (
	"strconv"
	"strings"

	"github.com/axispx/zeta/internal/styles"
)

const maxDiffUILines = 40 // colored diff body lines shown under an edit header

type diffLineKind int

const (
	diffSkip diffLineKind = iota // --- / +++ / @@
	diffAdd
	diffDel
	diffCtx
	diffMeta // \ No newline at end of file
	diffOther
)

type diffLine struct {
	kind diffLineKind
	text string // content without the leading +/-/space marker when applicable
	raw  string
}

// parseUnifiedDiff classifies unified-diff lines once for counting and rendering.
func parseUnifiedDiff(diff string) []diffLine {
	diff = strings.TrimRight(diff, "\n")
	if diff == "" {
		return nil
	}
	rawLines := strings.Split(diff, "\n")
	out := make([]diffLine, 0, len(rawLines))
	for _, line := range rawLines {
		switch {
		case strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "@@"):
			out = append(out, diffLine{kind: diffSkip, raw: line})
		case strings.HasPrefix(line, `\ No newline`):
			out = append(out, diffLine{kind: diffMeta, raw: line})
		case strings.HasPrefix(line, "+"):
			out = append(out, diffLine{kind: diffAdd, text: line[1:], raw: line})
		case strings.HasPrefix(line, "-"):
			out = append(out, diffLine{kind: diffDel, text: line[1:], raw: line})
		case strings.HasPrefix(line, " "):
			out = append(out, diffLine{kind: diffCtx, text: line[1:], raw: line})
		default:
			out = append(out, diffLine{kind: diffOther, raw: line})
		}
	}
	return out
}

// formatUnifiedDiff colors a unified diff and counts +/- lines in one pass.
// File headers (---/+++) and hunk headers (@@) are omitted from the body.
// Gutter signs (+/−/space) are separated from content with a space so markdown
// list markers (and similar) don't glue into "+-" / "--".
// Long diffs are capped at maxDiffUILines (tool result itself is already byte-truncated).
func formatUnifiedDiff(diff string) (adds, dels int, body string) {
	lines := parseUnifiedDiff(diff)
	if len(lines) == 0 {
		return 0, 0, ""
	}
	var b strings.Builder
	shown := 0
	skipped := 0
	for _, line := range lines {
		switch line.kind {
		case diffAdd:
			adds++
		case diffDel:
			dels++
		case diffSkip:
			continue
		}
		if shown >= maxDiffUILines {
			skipped++
			continue
		}
		if shown > 0 {
			b.WriteByte('\n')
		}
		shown++
		switch line.kind {
		case diffAdd:
			b.WriteString(styles.DiffAdd.Render("+ " + line.text))
		case diffDel:
			b.WriteString(styles.DiffDel.Render("- " + line.text))
		case diffCtx:
			b.WriteString("  " + line.text)
		case diffMeta:
			b.WriteString(styles.DiffMeta.Render(line.raw))
		default:
			b.WriteString(line.raw)
		}
	}
	if shown == 0 {
		return adds, dels, ""
	}
	if skipped > 0 {
		b.WriteByte('\n')
		b.WriteString(styles.DiffMeta.Render("… " + strconv.Itoa(skipped) + " more lines"))
	}
	return adds, dels, b.String()
}

func renderUnifiedDiff(diff string) string {
	_, _, body := formatUnifiedDiff(diff)
	return body
}

func countDiffLines(diff string) (adds, dels int) {
	for _, line := range parseUnifiedDiff(diff) {
		switch line.kind {
		case diffAdd:
			adds++
		case diffDel:
			dels++
		}
	}
	return adds, dels
}
