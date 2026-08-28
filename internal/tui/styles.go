package tui

import "github.com/charmbracelet/lipgloss"

// GitHub Dark palette
var (
	colorBg     = lipgloss.Color("#0d1117")
	colorText   = lipgloss.Color("#c9d1d9")
	colorMuted  = lipgloss.Color("#6e7681")
	colorAccent = lipgloss.Color("#58a6ff")
	colorGreen  = lipgloss.Color("#3fb950")
	colorRed    = lipgloss.Color("#f85149")
	colorYellow = lipgloss.Color("#f0e68c")
	colorBorder = lipgloss.Color("#21262d")
	colorSelBg      = lipgloss.Color("#161b22") // subtle: unused legacy shade
	colorSelSidebar = lipgloss.Color("#2d333b") // visible: sidebar selection
	colorSelCursor  = lipgloss.Color("#343b45") // logs cursor line
	colorVisBg      = lipgloss.Color("#1f3a5f")
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

	// Background-only highlight; no border so Width(width) fills cleanly.
	styleSelectedSidebar = lipgloss.NewStyle().
				Background(colorSelBg)

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
