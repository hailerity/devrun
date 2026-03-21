package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type headerBar struct{}

func (h headerBar) render(total, running, frame int, spinning bool, width int) string {
	left := styleAccent.Bold(true).Render("⬡ procet")

	indicator := "●"
	if spinning {
		indicator = spinFrames[frame%len(spinFrames)]
	}
	right := styleMuted.Render(fmt.Sprintf("%d services · %d running · %s", total, running, indicator))

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().
		Width(width).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Render(line)
}
