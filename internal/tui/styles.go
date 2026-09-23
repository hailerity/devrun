package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The palette is adaptive: each colour is a light/dark pair, and lipgloss picks
// the side that matches the terminal's background. The dark side is GitHub Dark
// (the original look); the light side is GitHub Light, so text stays readable on
// a light terminal instead of rendering pale grey on white.
var (
	colorText   = lipgloss.AdaptiveColor{Light: "#1f2328", Dark: "#c9d1d9"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#656d76", Dark: "#6e7681"}
	colorAccent = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	colorGreen  = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	colorRed    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	colorYellow = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#f0e68c"}
	// Not GitHub's border tone, unlike the rest of the palette: that assumes a
	// known page background, and on real terminal themes it measured 1.1–1.4:1,
	// far under the 3:1 a non-text element needs. Floor held by styles_test.go.
	colorBorder = lipgloss.AdaptiveColor{Light: "#868f99", Dark: "#767e89"}

	colorSelSidebar = lipgloss.AdaptiveColor{Light: "#eaeef2", Dark: "#2d333b"} // sidebar selection
	colorSelCursor  = lipgloss.AdaptiveColor{Light: "#e2e8ee", Dark: "#343b45"} // logs cursor line
	colorVisBg      = lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#1f3a5f"} // visual selection

	colorBar  = lipgloss.AdaptiveColor{Light: "#eaeef2", Dark: "#161b22"} // header / footer band
	colorChip = lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#30363d"} // key chips on that band
)

var (
	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleAccent = lipgloss.NewStyle().Foreground(colorAccent)
	styleGreen  = lipgloss.NewStyle().Foreground(colorGreen)
	styleRed    = lipgloss.NewStyle().Foreground(colorRed)
	styleYellow = lipgloss.NewStyle().Foreground(colorYellow)
	styleText   = lipgloss.NewStyle().Foreground(colorText)

	styleBorderH = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder)

	styleVisualLine = lipgloss.NewStyle().
			Background(colorVisBg).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorAccent)

	// styleSelectedLine highlights the logs cursor line: a full-width lighter
	// background plus an accent gutter bar. The bar is a separate cell that
	// embedded SGR resets in the log text cannot punch holes in, so the line
	// stays legible even when its content carries its own colour codes.
	styleSelectedLine = lipgloss.NewStyle().
				Background(colorSelCursor).
				BorderLeft(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(colorAccent).
				BorderBackground(colorSelCursor)
)

// barBackground paints row — one already-styled line — on the colorBar band,
// padded to width. The row is built from many separately styled segments, each
// ending in an SGR reset that would punch a hole in a background applied from
// outside, so the band is re-asserted after every reset instead. With colour
// disabled there is no sequence to emit and the row is returned unpainted.
func barBackground(row string, width int) string {
	hex := colorBar.Light
	if lipgloss.HasDarkBackground() {
		hex = colorBar.Dark
	}
	c := lipgloss.ColorProfile().Color(hex)
	if c == nil {
		return row
	}
	seq := c.Sequence(true)
	if seq == "" {
		return row
	}
	on := "\x1b[" + seq + "m"
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += strings.Repeat(" ", pad)
	}
	row = strings.ReplaceAll(row, "\x1b[0m", "\x1b[0m"+on)
	row = strings.ReplaceAll(row, "\x1b[m", "\x1b[m"+on)
	return on + row + "\x1b[0m"
}
