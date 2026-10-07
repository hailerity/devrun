package cli

import "github.com/charmbracelet/lipgloss"

var (
	styleLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6e7681"))
	styleValue  = lipgloss.NewStyle().Foreground(lipgloss.Color("#c9d1d9"))
	styleBold   = lipgloss.NewStyle().Foreground(lipgloss.Color("#c9d1d9")).Bold(true)
	styleGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	styleRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
	styleYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922"))
	styleAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
)
