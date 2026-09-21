package tui

import "github.com/charmbracelet/lipgloss"

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
	colorBorder = lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#21262d"}

	colorSelSidebar = lipgloss.AdaptiveColor{Light: "#eaeef2", Dark: "#2d333b"} // sidebar selection
	colorSelCursor  = lipgloss.AdaptiveColor{Light: "#e2e8ee", Dark: "#343b45"} // logs cursor line
	colorVisBg      = lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#1f3a5f"} // visual selection
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
