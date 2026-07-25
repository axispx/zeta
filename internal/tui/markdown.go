package tui

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	glamstyles "charm.land/glamour/v2/styles"
	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"

	"github.com/axispx/zeta/internal/styles"
)

// zetaChromaTheme is native with Error/Text/Background cleared so unlabeled
// fences (diagrams, trees) use the terminal default fg instead of chroma's
// Error tint and forced white Text. Labeled fences keep keyword colors.
const zetaChromaTheme = "zeta"

func init() {
	s, err := chromastyles.Get("native").Builder().
		Add(chroma.Error, "noinherit").
		Add(chroma.Text, "noinherit").
		Add(chroma.Background, "noinherit").
		Build()
	if err != nil {
		panic("zeta chroma theme: " + err.Error())
	}
	s.Name = zetaChromaTheme
	chromastyles.Register(s)
}

// agentMDStyle is ASCIIStyleConfig with zero margins (Transcript already insets).
// Prose uses the terminal default fg; accents use the 16-color palette so themes apply.
func agentMDStyle() ansi.StyleConfig {
	dim := styles.DimANSI
	blue := styles.BlueANSI
	cyan := styles.CyanANSI
	green := styles.GreenANSI

	s := glamstyles.ASCIIStyleConfig
	s.Document.Margin = uintPtr(0)
	s.Document.BlockPrefix = ""
	s.Document.BlockSuffix = ""
	s.Document.StylePrimitive.Color = nil // terminal default fg
	s.CodeBlock.Margin = uintPtr(0)
	s.CodeBlock.Theme = zetaChromaTheme
	s.CodeBlock.StylePrimitive.Color = nil
	s.List.LevelIndent = 2

	s.Heading = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "\n",
			Color:       &blue,
			Bold:        boolPtr(true),
		},
	}
	// No hash prefixes — title weight comes from color+bold alone.
	s.H1 = ansi.StyleBlock{}
	s.H2 = ansi.StyleBlock{}
	s.H3 = ansi.StyleBlock{}
	s.H4 = ansi.StyleBlock{}
	s.H5 = ansi.StyleBlock{}
	s.H6 = ansi.StyleBlock{}

	// Hyphen bullets; bracket checkboxes for tasks.
	s.Item = ansi.StylePrimitive{BlockPrefix: "- ", Color: &blue}
	s.Enumeration = ansi.StylePrimitive{BlockPrefix: ". ", Color: &blue}
	s.Task = ansi.StyleTask{
		StylePrimitive: ansi.StylePrimitive{Color: &green},
		Ticked:         "[✓] ",
		Unticked:       "[ ] ",
	}

	s.BlockQuote = ansi.StyleBlock{
		Indent:      uintPtr(1),
		IndentToken: stringPtr("│ "),
		StylePrimitive: ansi.StylePrimitive{
			Color:  &dim,
			Italic: boolPtr(true),
		},
	}
	s.Strikethrough = ansi.StylePrimitive{CrossedOut: boolPtr(true)}
	s.Emph = ansi.StylePrimitive{Italic: boolPtr(true)}
	s.Strong = ansi.StylePrimitive{Bold: boolPtr(true)} // bold only — terminal brightens via theme
	s.HorizontalRule = ansi.StylePrimitive{
		Color:  &dim,
		Format: "\n────────\n",
	}
	s.Link = ansi.StylePrimitive{Color: &blue, Underline: boolPtr(true)}
	s.LinkText = ansi.StylePrimitive{Bold: boolPtr(true)}
	s.Image = ansi.StylePrimitive{Color: &blue, Underline: boolPtr(true)}
	s.ImageText = ansi.StylePrimitive{Color: &dim, Format: "Image: {{.text}} →"}
	s.Code = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: &cyan},
	}
	s.Table = ansi.StyleTable{
		CenterSeparator: stringPtr("┼"),
		ColumnSeparator: stringPtr("│"),
		RowSeparator:    stringPtr("─"),
	}
	return s
}

// Reuse one TermRenderer per wrap width (glamour rebuild is relatively expensive).
var (
	mdMu sync.Mutex
	mdW  int
	mdR  *glamour.TermRenderer
)

func agentRenderer(width int) (*glamour.TermRenderer, error) {
	wrap := width
	if wrap <= 0 {
		wrap = 80
	}
	mdMu.Lock()
	defer mdMu.Unlock()
	if mdR != nil && mdW == wrap {
		return mdR, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(agentMDStyle()),
		glamour.WithWordWrap(wrap),
		glamour.WithChromaFormatter("terminal16"),
	)
	if err != nil {
		return nil, err
	}
	mdR = r
	mdW = wrap
	return r, nil
}

// renderMarkdown styles markdown for the agent transcript at the given wrap width.
// On failure it falls back to plain AgentMsg wrapping.
func renderMarkdown(text string, width int) string {
	if text == "" {
		return ""
	}
	r, err := agentRenderer(width)
	if err != nil {
		return plainAgent(text, width)
	}
	out, err := r.Render(text)
	if err != nil {
		return plainAgent(text, width)
	}
	return strings.TrimSpace(out)
}

// fence is a markdown fence line (``` / ~~~), optionally with an info string.
type fence struct {
	marker string // "```" or "~~~"
	lang   string // info string; empty on closers per CommonMark
}

func (f fence) closes(open fence) bool {
	return f.marker == open.marker && f.lang == ""
}

func parseFenceLine(line string) (fence, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	var marker string
	switch {
	case strings.HasPrefix(trimmed, "```"):
		marker = "```"
	case strings.HasPrefix(trimmed, "~~~"):
		marker = "~~~"
	default:
		return fence{}, false
	}
	return fence{
		marker: marker,
		lang:   strings.TrimSpace(trimmed[len(marker):]),
	}, true
}

// streamSplit divides streaming markdown into a settled prefix and an
// in-progress tail. Settled text is safe for glamour; tail stays plain.
//
// Settled ≡ text before an open fence, else text before the last "\n\n".
// Settled never keeps a trailing blank line — the joiner always inserts "\n\n".
func streamSplit(text string) (settled, tail string) {
	if text == "" {
		return "", ""
	}
	if idx := unmatchedFenceStart(text); idx >= 0 {
		return strings.TrimRight(text[:idx], "\n"), text[idx:]
	}
	idx := strings.LastIndex(text, "\n\n")
	if idx < 0 {
		return "", text
	}
	settled = text[:idx]
	tail = text[idx+2:]
	if tail == "" {
		return text, ""
	}
	return settled, tail
}

// unmatchedFenceStart returns the byte offset of an unclosed fence, or -1.
// Open/close matching is marker-aware; info strings never close (CommonMark).
func unmatchedFenceStart(text string) int {
	openAt := -1
	var open fence
	offset := 0
	rest := text
	for {
		line, after, found := strings.Cut(rest, "\n")
		if f, ok := parseFenceLine(line); ok {
			if openAt < 0 {
				openAt = offset
				open = f
			} else if f.closes(open) {
				openAt = -1
			}
		}
		if !found {
			break
		}
		offset += len(line) + 1
		rest = after
	}
	return openAt
}

// plainAgent wraps agent text without markdown.
// lipgloss Width matches glamour WordWrap at margin 0 for plain prose.
func plainAgent(text string, width int) string {
	s := styles.AgentMsg
	if width > 0 {
		s = s.Width(width)
	}
	return s.Render(text)
}

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }
func uintPtr(u uint) *uint       { return &u }
