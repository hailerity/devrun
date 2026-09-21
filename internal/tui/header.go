package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type headerBar struct{}

// render draws the one-row header: the app name and the config in scope on the
// left, the project-wide running count — plus a red crashed count when anything
// is down — and the poll indicator on the right.
func (h headerBar) render(source string, total, running, crashed, frame int, spinning bool, width int) string {
	left := " " + styleAccent.Bold(true).Render("⬡ devrun")
	if source != "" {
		left += "  " + styleMuted.Render(source)
	}

	count := styleMuted
	if running > 0 {
		count = styleGreen
	}
	right := count.Render(fmt.Sprintf("%d/%d running", running, total))
	if crashed > 0 {
		right += "  " + styleRed.Render(fmt.Sprintf("✖ %d crashed", crashed))
	}
	indicator := " "
	if spinning {
		indicator = spinFrames[frame%len(spinFrames)]
	}
	// The poll indicator owns the last column, above the main pane's corner, so
	// the counts line up with the pane edge whether or not it is spinning.
	right += " " + styleMuted.Render(indicator)

	// The counts matter more than the config label: drop the label before
	// letting the line outgrow the terminal.
	if lipgloss.Width(left)+1+lipgloss.Width(right) > width {
		left = " " + styleAccent.Bold(true).Render("⬡ devrun")
	}
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	// lipgloss's Width() only pads a short line, it never caps a long one, and
	// an overlong header wraps on the real terminal and scrolls the layout —
	// so truncate as the last word on width.
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, width, "")
}
