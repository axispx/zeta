// Package styles defines the lipgloss tokens, chrome and banner the TUI draws with.
package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// 16-color ANSI indexes — actual hues come from the terminal colorscheme.
const (
	DimANSI    = "8"  // bright black / gray
	RedANSI    = "9"  // bright red
	GreenANSI  = "10" // bright green
	YellowANSI = "11" // bright yellow
	BlueANSI   = "12" // bright blue
	CyanANSI   = "14" // bright cyan
	WhiteANSI  = "15" // bright white

	// Panel lift from terminal bg (Charm Lighten/Darken).
	// Only user bubbles are lifted; panels and overlays use the terminal bg.
	promptPanelLift = 0.14

	// OverlayPadRight matches OverlayPanel's right padding (lipgloss Width includes it).
	OverlayPadRight = 1
)

// Palette colors are the terminal's 16 ANSI slots, so themes apply.
var (
	Dim    = lipgloss.Color(DimANSI)
	Red    = lipgloss.Color(RedANSI)
	Green  = lipgloss.Color(GreenANSI)
	Yellow = lipgloss.Color(YellowANSI)
	Blue   = lipgloss.Color(BlueANSI)
	Cyan   = lipgloss.Color(CyanANSI)
	White  = lipgloss.Color(WhiteANSI)

	// Banner is the startup banner. Prose styles omit Foreground so the
	// terminal default fg applies.
	Banner = lipgloss.NewStyle().Bold(true).Foreground(Blue)

	// Mascot is the one fixed truecolor in the UI: a warm snow white that does
	// not follow the terminal theme, so the yeti keeps its look everywhere.
	Mascot = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ece9e2"))

	AgentMsg = lipgloss.NewStyle()

	SystemMsg = lipgloss.NewStyle().
			Foreground(Dim).
			Italic(true)

	ErrorMsg = lipgloss.NewStyle().
			Bold(true).
			Underline(true)

	ToolMsg = lipgloss.NewStyle().
		Foreground(Dim).
		Faint(true)

	// ThinkingMsg is the live reasoning tail (dim, ephemeral).
	ThinkingMsg = ToolMsg.Italic(true)

	// DiffAdd, DiffDel and DiffMeta style diff lines with the 16-color palette
	// so terminal themes apply.
	DiffAdd  = lipgloss.NewStyle().Foreground(Green)
	DiffDel  = lipgloss.NewStyle().Foreground(Red)
	DiffMeta = lipgloss.NewStyle().Foreground(Dim).Faint(true)
	DiffFile = lipgloss.NewStyle().Foreground(Dim) // edit header path (full weight vs ToolMsg)

	Prompt = lipgloss.NewStyle().
		Bold(true)

	Placeholder = lipgloss.NewStyle().
			Italic(true).
			Faint(true)

	// Transcript horizontal padding must match ContentInset used in layout.
	Transcript = lipgloss.NewStyle().
			Padding(0, ContentInset)

	// Selection highlights drag-selected transcript cells (app-level copy).
	Selection = lipgloss.NewStyle().
			Reverse(true)

	// FollowUpsHint styles queued follow-ups above the input.
	FollowUpsHint = lipgloss.NewStyle().Foreground(Dim)

	// OutsideWarn flags a path outside the workspace in permission prompts.
	OutsideWarn = lipgloss.NewStyle().Bold(true).Foreground(Yellow)

	// OverlayRow is an overlay / accent-list row (command palette, model
	// overlay, session picker). It uses default terminal fg (same as input text).
	OverlayRow         = lipgloss.NewStyle()
	OverlayHint        = lipgloss.NewStyle().Foreground(Dim).Italic(true)
	AccentRowSelected  = lipgloss.NewStyle().Foreground(Green)  // keyboard selection
	AccentRowCurrent   = lipgloss.NewStyle().Foreground(Yellow) // configured / open item
	AccentHintSelected = lipgloss.NewStyle().Foreground(Green).Italic(true)
	AccentHintCurrent  = lipgloss.NewStyle().Foreground(Yellow).Italic(true)
	OverlayHeader      = lipgloss.NewStyle().Bold(true)
	// Kbd is keyboard-shortcut chrome (inline-code look).
	Kbd = lipgloss.NewStyle().Foreground(Cyan)
	// HintText is the label beside a kbd in footer hints.
	HintText = lipgloss.NewStyle().Foreground(White)
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
		Prompt: PanelFromTerminal(termBg, dark, promptPanelLift),
	}
}

// InputBox is the bordered frame around the composer.
func (c Chrome) InputBox() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false).
		BorderForeground(Dim).
		PaddingRight(1)
}

// UserMsg styles a user message bubble in the transcript.
func (c Chrome) UserMsg() lipgloss.Style {
	s := lipgloss.NewStyle().Padding(0, 1)
	if c.Prompt != nil {
		s = s.Background(c.Prompt)
	}
	return s
}

// OverlayPanel is the fill behind command/model lists above the input.
func (c Chrome) OverlayPanel() lipgloss.Style {
	s := lipgloss.NewStyle().Padding(1, OverlayPadRight, 0, 0)
	if c.Input != nil {
		s = s.Background(c.Input)
	}
	return s
}

// OverlayInk is accent-list row styling. Gap carries panel fill so pad cells
// don't punch through to the terminal background.
type OverlayInk struct {
	Row, Hint              lipgloss.Style
	Selected, SelectedHint lipgloss.Style
	Current, CurrentHint   lipgloss.Style
	Header                 lipgloss.Style
	Gap                    lipgloss.Style
	Kbd, HintText          lipgloss.Style
	// Warn marks a prompt payload that needs attention (a path outside the
	// workspace). Panel fill is baked in like every other row style: a raw
	// styles.OutsideWarn would paint its own background band across the panel.
	Warn lipgloss.Style
}

// OverlayInk returns row styles with the input-panel fill baked in.
func (c Chrome) OverlayInk() OverlayInk {
	return OverlayInk{
		Row:          c.withPanelBG(OverlayRow),
		Hint:         c.withPanelBG(OverlayHint),
		Selected:     c.withPanelBG(AccentRowSelected),
		SelectedHint: c.withPanelBG(AccentHintSelected),
		Current:      c.withPanelBG(AccentRowCurrent),
		CurrentHint:  c.withPanelBG(AccentHintCurrent),
		Header:       c.withPanelBG(OverlayHeader),
		Gap:          c.withPanelBG(lipgloss.NewStyle()),
		Kbd:          c.withPanelBG(Kbd),
		HintText:     c.withPanelBG(HintText),
		Warn:         c.withPanelBG(OutsideWarn),
	}
}

// PlainOverlayInk is accent-row styling without panel fill (full-screen pickers).
func PlainOverlayInk() OverlayInk {
	return OverlayInk{
		Row:          OverlayRow,
		Hint:         OverlayHint,
		Selected:     AccentRowSelected,
		SelectedHint: AccentHintSelected,
		Current:      AccentRowCurrent,
		CurrentHint:  AccentHintCurrent,
		Header:       OverlayHeader,
		Gap:          lipgloss.NewStyle(),
		Kbd:          Kbd,
		HintText:     HintText,
		Warn:         OutsideWarn,
	}
}

// HintKbd renders "label key" using HintText + Kbd (panel fill baked in).
func (ink OverlayInk) HintKbd(label, key string) string {
	// Keep the separating space inside a styled span so it inherits panel BG.
	return ink.HintText.Render(label+" ") + ink.Kbd.Render(key)
}

func (c Chrome) withPanelBG(s lipgloss.Style) lipgloss.Style {
	if c.Input == nil {
		return s
	}
	return s.Background(c.Input)
}

// MascotArt is the yeti face: a 20x8 pixel grid in quadrant blocks (2x2 pixels
// per cell). The eyes are gaps, so the terminal background shows through.
const MascotArt = `
▗▙██████▟▖
█▌▐████▌▐█
▜▛██▜▛██▜▛
`

// ContentInset sets the horizontal inset (columns per side) shared by transcript padding and wrap width.
const ContentInset = 1

// Input box geometry (lipgloss v2 Width is the total rendered width):
//
//	style.Width = terminal W - 2*InputMarginH
//	textarea    = style.Width - InputChromeH
//	rendered H  = textarea H + InputChromeV
const (
	InputPadV      = 2 // top + bottom rule (1 each)
	InputPadH      = 2 // left + right (1 each)
	InputChromeH   = InputPadH
	InputChromeV   = InputPadV
	InputMarginH   = 1 // columns of empty space each side
	InputMarginB   = 0 // rows below input before footer
	GapBeforeInput = 1
)
