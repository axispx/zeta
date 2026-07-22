package tui

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	glamstyles "charm.land/glamour/v2/styles"

	"github.com/axispx/zeta/internal/styles"
)

// agentMDStyle is ASCIIStyleConfig with zero margins (Transcript already insets)
// and monochrome styling aligned to styles.FgANSI / styles.DimANSI.
func agentMDStyle() ansi.StyleConfig {
	fg := styles.FgANSI
	dim := styles.DimANSI
	s := glamstyles.ASCIIStyleConfig
	s.Document.Margin = uintPtr(0)
	s.Document.BlockPrefix = ""
	s.Document.BlockSuffix = ""
	s.Document.StylePrimitive.Color = &fg
	s.CodeBlock.Margin = uintPtr(0)
	s.CodeBlock.Theme = "native"
	s.List.LevelIndent = 2
	s.Heading = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "\n",
			Color:       &fg,
			Bold:        boolPtr(true),
		},
	}
	s.H5 = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "##### ",
			Color:  &dim,
			Bold:   boolPtr(false),
		},
	}
	s.H6 = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "###### ",
			Color:  &dim,
			Bold:   boolPtr(false),
		},
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
	s.Strong = ansi.StylePrimitive{Bold: boolPtr(true)}
	s.HorizontalRule = ansi.StylePrimitive{
		Color:  &dim,
		Format: "\n────────\n",
	}
	s.Link = ansi.StylePrimitive{Color: &dim, Underline: boolPtr(true)}
	s.LinkText = ansi.StylePrimitive{Bold: boolPtr(true)}
	s.Image = ansi.StylePrimitive{Color: &dim, Underline: boolPtr(true)}
	s.ImageText = ansi.StylePrimitive{Color: &dim, Format: "Image: {{.text}} →"}
	// Inline code: dim fg only — no bg chip (keeps agent transcript monochrome).
	s.Code = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color: &dim,
		},
	}
	s.Table = ansi.StyleTable{
		CenterSeparator: stringPtr("┼"),
		ColumnSeparator: stringPtr("│"),
		RowSeparator:    stringPtr("─"),
	}
	s.Task = ansi.StyleTask{Ticked: "[✓] ", Unticked: "[ ] "}
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
