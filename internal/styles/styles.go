package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// 16-color ANSI indexes — actual hues come from the terminal colorscheme.
const (
	DimANSI    = "8"  // bright black / gray
	GreenANSI  = "10" // bright green
	YellowANSI = "11" // bright yellow
	BlueANSI   = "12" // bright blue
	CyanANSI   = "14" // bright cyan

	// Panel lift from terminal bg (Charm Lighten/Darken).
	// Input is subtler; user bubbles are more elevated.
	inputPanelLift  = 0.08
	promptPanelLift = 0.14
)

var (
	Dim    = lipgloss.Color(DimANSI)
	Green  = lipgloss.Color(GreenANSI)
	Yellow = lipgloss.Color(YellowANSI)
	Blue   = lipgloss.Color(BlueANSI)
	Cyan   = lipgloss.Color(CyanANSI)

	// Prose styles omit Foreground so the terminal default fg applies.
	Banner = lipgloss.NewStyle().Bold(true).Foreground(Blue)

	AgentMsg = lipgloss.NewStyle()

	SystemMsg = lipgloss.NewStyle().
			Foreground(Dim).
			Italic(true)

	ErrorMsg = lipgloss.NewStyle().
			Bold(true).
			Underline(true)

	ToolMsg = lipgloss.NewStyle().
		Foreground(Dim)

	Prompt = lipgloss.NewStyle().
		Bold(true)

	Placeholder = lipgloss.NewStyle().
			Italic(true).
			Faint(true)

	// Transcript horizontal padding must match ContentInset used in layout.
	Transcript = lipgloss.NewStyle().
			Padding(0, ContentInset)

	ScrollTrack = lipgloss.NewStyle().
			Foreground(Dim)

	ScrollThumb = lipgloss.NewStyle().
			Bold(true)

	// Footer mode accents (Build / Ask / Plan). Mapping from mode → style lives in tui.
	StyleModeBuild = lipgloss.NewStyle().Bold(true).Foreground(Blue)
	StyleModeAsk   = lipgloss.NewStyle().Bold(true).Foreground(Green)
	StyleModePlan  = lipgloss.NewStyle().Bold(true).Foreground(Yellow)

	// Overlay / accent-list rows (command palette, model overlay, session picker).
	OverlayRow         = lipgloss.NewStyle().Foreground(Dim)
	OverlayHint        = lipgloss.NewStyle().Foreground(Dim).Italic(true)
	AccentRowSelected  = lipgloss.NewStyle().Foreground(Green)  // keyboard selection
	AccentRowCurrent   = lipgloss.NewStyle().Foreground(Yellow) // configured / open item
	AccentHintSelected = lipgloss.NewStyle().Foreground(Green).Italic(true)
	AccentHintCurrent  = lipgloss.NewStyle().Foreground(Yellow).Italic(true)
	OverlayHeader      = lipgloss.NewStyle().Bold(true)
	// OverlayHintBar is the pinned footer in full-screen pickers (border top, flush bottom).
	OverlayHintBar = lipgloss.NewStyle().
			Foreground(Dim).
			Italic(true).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(Dim)
)

// PanelFromTerminal returns a shade of termBg for panels: lighter on dark
// terminals, darker on light ones.
func PanelFromTerminal(termBg color.Color, dark bool, lift float64) color.Color {
	if dark {
		return lipgloss.Lighten(termBg, lift)
	}
	return lipgloss.Darken(termBg, lift)
}

// Chrome holds terminal-derived panel colors. Zero value is safe before
// BackgroundColorMsg (padding-only panels, untinted banner).
type Chrome struct {
	Input  color.Color
	Prompt color.Color
}

// NewChrome derives panel fills from the live terminal background.
func NewChrome(termBg color.Color, dark bool) Chrome {
	return Chrome{
		Input:  PanelFromTerminal(termBg, dark, inputPanelLift),
		Prompt: PanelFromTerminal(termBg, dark, promptPanelLift),
	}
}

func (c Chrome) InputBox() lipgloss.Style {
	s := lipgloss.NewStyle().Padding(1, 1)
	if c.Input != nil {
		s = s.Background(c.Input)
	}
	return s
}

func (c Chrome) UserMsg() lipgloss.Style {
	s := lipgloss.NewStyle().Padding(1, 1)
	if c.Prompt != nil {
		s = s.Background(c.Prompt)
	}
	return s
}

// BannerArt is the ZETA shadow block logo.
const BannerArt = `
███████╗███████╗████████╗ █████╗
╚══███╔╝██╔════╝╚══██╔══╝██╔══██╗
  ███╔╝ █████╗     ██║   ███████║
 ███╔╝  ██╔══╝     ██║   ██╔══██║
███████╗███████╗   ██║   ██║  ██║
╚══════╝╚══════╝   ╚═╝   ╚═╝  ╚═╝
`

// Horizontal inset (columns per side) shared by transcript padding and wrap width.
const ContentInset = 1

// Input box geometry (lipgloss v2 Width is the total rendered width):
//
//	style.Width = terminal W - 2*InputMarginH
//	textarea    = style.Width - InputChromeH
//	rendered H  = textarea H + InputChromeV
const (
	InputPadV      = 2 // top + bottom (1 each)
	InputPadH      = 2 // left + right (1 each)
	InputChromeH   = InputPadH
	InputChromeV   = InputPadV
	InputMarginH   = 1 // columns of empty space each side
	InputMarginB   = 1 // rows below input before footer
	GapBeforeInput = 1
)
