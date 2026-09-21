package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// paneFrame draws a rounded border around a pane with labels set into the
// border itself: a title and a right-hand label on the top edge, a left and a
// right status on the bottom edge. The border takes the accent colour while the
// pane holds focus, which is the TUI's one focus cue.
//
// lipgloss's own Border() cannot embed text in an edge, so the frame is
// assembled by hand. Every row it returns is exactly the outer width and there
// are exactly the outer height of them — the caller joins panes horizontally
// and a single short or long row would shear the whole layout.
type paneFrame struct {
	title      string // top-left label, already styled
	titleRight string // top-right label, already styled
	footLeft   string // bottom-left status, already styled
	footRight  string // bottom-right status, already styled
	focused    bool
	padLeft    int // blank columns between the left border and the content
}

// paneChrome is how many rows and columns the border itself takes.
const paneChrome = 2

// innerSize returns the content area a pane of outer size w×h leaves.
func (f paneFrame) innerSize(w, h int) (int, int) {
	return max(0, w-paneChrome-f.padLeft), max(0, h-paneChrome)
}

func (f paneFrame) render(content string, w, h int) string {
	if w < paneChrome || h < paneChrome {
		return ""
	}
	border := lipgloss.NewStyle().Foreground(colorBorder)
	if f.focused {
		border = lipgloss.NewStyle().Foreground(colorAccent)
	}
	iw, ih := f.innerSize(w, h)

	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}
	if len(lines) > ih {
		lines = lines[:ih]
	}

	side := border.Render("│")
	pad := strings.Repeat(" ", f.padLeft)
	rows := make([]string, 0, h)
	rows = append(rows, f.edge(border, "╭", "╮", f.title, f.titleRight, w))
	for i := 0; i < ih; i++ {
		line := ""
		if i < len(lines) {
			// Truncate does not reset an open SGR sequence unless it cuts, and a
			// log line may end mid-colour — close it so nothing bleeds into the
			// right border or the neighbouring pane.
			line = ansi.Truncate(lines[i], iw, "") + "\x1b[m"
		}
		if gap := iw - lipgloss.Width(line); gap > 0 {
			line += strings.Repeat(" ", gap)
		}
		rows = append(rows, side+pad+line+side)
	}
	rows = append(rows, f.edge(border, "╰", "╯", f.footLeft, f.footRight, w))
	return strings.Join(rows, "\n")
}

// edge builds one horizontal border row: corner, a label near each end, and a
// rule filling the rest. When both labels do not fit, the right one is dropped
// first, then the left one is truncated — the left label names the pane.
func (f paneFrame) edge(border lipgloss.Style, cornerL, cornerR, left, right string, w int) string {
	const fixed = 4 // two corners + one rule cell inside each
	room := w - fixed
	if room < 0 {
		return border.Render(cornerL + strings.Repeat("─", max(0, w-2)) + cornerR)
	}

	labelW := func(s string) int {
		if s == "" {
			return 0
		}
		return lipgloss.Width(s) + 2 // a space either side
	}
	if labelW(left)+labelW(right) > room {
		right = ""
	}
	if labelW(left) > room {
		left = ansi.Truncate(left, max(0, room-2), "…")
		if room < 3 {
			left = ""
		}
	}

	wrap := func(s string) string {
		if s == "" {
			return ""
		}
		return " " + s + "\x1b[m "
	}
	fill := room - labelW(left) - labelW(right)
	return border.Render(cornerL+"─") + wrap(left) +
		border.Render(strings.Repeat("─", max(0, fill))) +
		wrap(right) + border.Render("─"+cornerR)
}
