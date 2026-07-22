package styles

import (
	"image/color"
	"testing"
)

func TestPanelFromTerminalLightensDarkBg(t *testing.T) {
	bg := color.RGBA{R: 0x1a, G: 0x1b, B: 0x26, A: 0xff}
	panel := PanelFromTerminal(bg, true, inputPanelLift)
	pr, pg, pb, _ := panel.RGBA()
	br, bgc, bb, _ := bg.RGBA()
	if pr>>8 <= br>>8 || pg>>8 <= bgc>>8 || pb>>8 <= bb>>8 {
		t.Fatalf("expected lighter panel got bg=%v panel=%v", bg, panel)
	}
}

func TestPanelFromTerminalDarkensLightBg(t *testing.T) {
	bg := color.RGBA{R: 0xf5, G: 0xf5, B: 0xf5, A: 0xff}
	panel := PanelFromTerminal(bg, false, inputPanelLift)
	pr, pg, pb, _ := panel.RGBA()
	br, bgc, bb, _ := bg.RGBA()
	if pr>>8 >= br>>8 || pg>>8 >= bgc>>8 || pb>>8 >= bb>>8 {
		t.Fatalf("expected darker panel got bg=%v panel=%v", bg, panel)
	}
}

func TestPromptPanelMoreElevatedThanInput(t *testing.T) {
	bg := color.RGBA{R: 0x1a, G: 0x1b, B: 0x26, A: 0xff}
	c := NewChrome(bg, true)
	ir, _, _, _ := c.Input.RGBA()
	pr, _, _, _ := c.Prompt.RGBA()
	if pr>>8 <= ir>>8 {
		t.Fatalf("prompt panel should be brighter than input: input=%v prompt=%v", c.Input, c.Prompt)
	}
}

func TestChromeZeroValueSafe(t *testing.T) {
	var c Chrome
	_ = c.InputBox()
	_ = c.UserMsg()
	_ = c.OverlayPanel()
	_ = c.OverlayInk()
}

func TestChromeDerivedStyles(t *testing.T) {
	c := NewChrome(color.RGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}, true)
	_ = c.InputBox()
	_ = c.UserMsg()
	_ = c.OverlayPanel()
	_ = c.OverlayInk()
}
