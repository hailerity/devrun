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
	colorSelBg  = lipgloss.Color("#161b22")
	colorVisBg  = lipgloss.Color("#1f3a5f")
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

	styleSelectedSidebar = lipgloss.NewStyle().
				Background(colorSelBg).
				BorderLeft(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(colorAccent)

	styleVisualLine = lipgloss.NewStyle().
			Background(colorVisBg).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorAccent)

	styleSelectedLine = lipgloss.NewStyle().
				Background(colorSelBg)
)
