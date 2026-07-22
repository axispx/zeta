package styles

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
)

// Monochrome palette — terminal default fg/bg only.
var (
	Fg       = lipgloss.Color("15")                                                                    // white
	Dim      = lipgloss.Color("8")                                                                     // bright black / gray
	BgInput  = compat.AdaptiveColor{Light: lipgloss.Color("#f0f0f0"), Dark: lipgloss.Color("#333333")} // input panel
	BgPrompt = compat.AdaptiveColor{Light: lipgloss.Color("#e2e4e8"), Dark: lipgloss.Color("#3d3b40")} // cool blue-gray (from swatch)

	Banner = lipgloss.NewStyle().
		Foreground(Fg).
		Bold(true)

	// UserMsg: prompt shown as a tinted block in the transcript (no role label).
	UserMsg = lipgloss.NewStyle().
		Foreground(Fg).
		Background(BgPrompt).
		Padding(1, 1)

	AgentMsg = lipgloss.NewStyle().
			Foreground(Fg)

	SystemMsg = lipgloss.NewStyle().
			Foreground(Dim).
			Italic(true)

	ErrorMsg = lipgloss.NewStyle().
			Foreground(Fg).
			Bold(true).
			Underline(true)

	// InputBox: Cursor-style filled panel (no border) + horizontal pad.
	InputBox = lipgloss.NewStyle().
			Background(BgInput).
			Padding(1, 1)

	Prompt = lipgloss.NewStyle().
		Foreground(Fg).
		Bold(true)

	Placeholder = lipgloss.NewStyle().
			Foreground(Dim)

	// Transcript horizontal padding must match ContentInset used in layout.
	Transcript = lipgloss.NewStyle().
			Padding(0, ContentInset)

	ScrollTrack = lipgloss.NewStyle().
			Foreground(Dim)

	ScrollThumb = lipgloss.NewStyle().
			Foreground(Fg)

	// Footer mode accents (Build / Ask / Plan). Mapping from mode → style lives in tui.
	StyleModeBuild = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")) // blue
	StyleModeAsk   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")) // green
	StyleModePlan  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")) // yellow

	// Overlay list rows (command palette, session picker).
	OverlayRow       = lipgloss.NewStyle().Foreground(Dim)
	OverlayRowActive = lipgloss.NewStyle().Foreground(Fg).Background(BgPrompt).Bold(true)
	OverlayHint      = lipgloss.NewStyle().Foreground(Dim).Italic(true)
	// OverlayHintBar is the pinned footer in full-screen pickers (border top, flush bottom).
	OverlayHintBar = lipgloss.NewStyle().
			Foreground(Dim).
			Italic(true).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(Dim)

	// Model picker row accents (Cursor model-list palette).
	ModelRowSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("#52A078")) // keyboard selection
	ModelRowCurrent  = lipgloss.NewStyle().Foreground(lipgloss.Color("#B5BD6B")) // configured model
	ModelHintCurrent = lipgloss.NewStyle().Foreground(lipgloss.Color("#B5BD6B")).Italic(true)
)

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
