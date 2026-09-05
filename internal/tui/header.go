package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type headerBar struct{}

func (h headerBar) render(total, running, frame int, spinning bool, width int) string {
	left := styleAccent.Bold(true).Render("⬡ devrun")

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
	// gap only ever grows to make the line reach `width`; clamping it to a
	// minimum of 1 above means a wide enough left+right can still leave the
	// line wider than width (a narrow terminal with a long service count, or
	// wide sidebar/detail label, say). lipgloss's Width() would only pad a
	// short line up to width, never cap a long one back down, and that gap
	// is exactly what let the footer's hint line wrap the real terminal and
	// scroll the header off screen — so truncate the same way here.
	line = ansi.Truncate(line, width, "")
	return lipgloss.NewStyle().
		Width(width).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Render(line)
}
