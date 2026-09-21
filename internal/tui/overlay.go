package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlay composites a modal box over the centre of the body. The panes stay
// on screen behind it, stripped of their colours and dimmed, so opening a modal
// does not blank the context it acts on — and the dimming says plainly that
// the panes are not taking input.
//
// The result is exactly w×h: a box taller or wider than the body is clipped to
// it rather than allowed to push the header and footer off the terminal.
func overlay(body, box string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	dim := lipgloss.NewStyle().Foreground(colorMuted).Faint(true)

	bg := strings.Split(body, "\n")
	boxRows := strings.Split(box, "\n")
	if len(boxRows) > h {
		boxRows = boxRows[:h]
	}
	boxW := 0
	for _, r := range boxRows {
		boxW = max(boxW, lipgloss.Width(r))
	}
	boxW = min(boxW, w)
	x, y := (w-boxW)/2, (h-len(boxRows))/2

	// fit returns plain text s as exactly n columns. Cutting through a wide
	// rune can come back a column short, so the pad is what guarantees width.
	fit := func(s string, n int) string {
		if n <= 0 {
			return ""
		}
		s = ansi.Truncate(s, n, "")
		return s + strings.Repeat(" ", max(0, n-lipgloss.Width(s)))
	}

	out := make([]string, h)
	for i := range out {
		row := ""
		if i < len(bg) {
			row = ansi.Strip(bg[i])
		}
		row = fit(row, w)
		if i < y || i >= y+len(boxRows) {
			out[i] = dim.Render(row)
			continue
		}
		// Pad from the truncated width, not the original: cutting a box row
		// through a wide rune leaves it a column short of boxW.
		boxRow := ansi.Truncate(boxRows[i-y], boxW, "")
		boxRow += "\x1b[m" + strings.Repeat(" ", max(0, boxW-lipgloss.Width(boxRow)))
		out[i] = dim.Render(fit(ansi.Cut(row, 0, x), x)) +
			boxRow +
			dim.Render(fit(ansi.Cut(row, x+boxW, w), w-x-boxW))
	}
	return strings.Join(out, "\n")
}
