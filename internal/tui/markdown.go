package tui

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	glamstyles "charm.land/glamour/v2/styles"

	"github.com/axispx/zeta/internal/styles"
)

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
	s.CodeBlock.Theme = "native"
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

	// Codex-style hyphen bullets; OpenCode-style bracket checkboxes.
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

// plainAgent wraps agent text without markdown (used while streaming).
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
