package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// helpPanel is the `?` overlay: the whole keymap, grouped by what the keys act
// on. The footer can only ever show a few hints; this is where the rest live.
type helpPanel struct{ open bool }

type helpGroup struct {
	title    string
	bindings []key.Binding
}

// helpGroups lays the keymap out for the overlay. It is built from the same
// key.Binding values handleKey matches against, so a rebinding or a reworded
// description shows up here without a second list to keep in step.
//
// The two columns are balanced on purpose, not grouped by theme alone: the
// overlay has no scrolling and overlay() hard-clips it to the body height, so
// its row count is a fixed budget and the taller column spends it. LOGS sits
// under MOVE rather than under SERVICES to keep the columns near the same
// height — TestHelp_ViewFitsASmallTerminal guards the total.
func helpGroups() [][]helpGroup {
	return [][]helpGroup{
		{ // left column
			{"MOVE", []key.Binding{keys.Up, keys.Down, keys.Tab, keys.Left, keys.Right, keys.Enter, keys.Top, keys.Bottom}},
			{"LOGS", []key.Binding{keys.Search, keys.Next, keys.Prev, keys.Follow, keys.Wrap, keys.Visual, keys.Copy}},
		},
		{ // right column
			{"SERVICES", []key.Binding{keys.Start, keys.Stop, keys.Restart, keys.StartAll, keys.StopAll, keys.Expose, keys.Target, keys.Filter, keys.Edit, keys.Remove}},
			{"OTHER", []key.Binding{keys.Escape, keys.Help, keys.Quit}},
		},
	}
}

func (h helpPanel) view() string {
	column := func(groups []helpGroup) string {
		keyW := 0
		for _, g := range groups {
			for _, b := range g.bindings {
				keyW = max(keyW, lipgloss.Width(b.Help().Key))
			}
		}
		var rows []string
		for i, g := range groups {
			if i > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, styleMuted.Render(g.title))
			for _, b := range g.bindings {
				rows = append(rows, styleText.Render(padRight(b.Help().Key, keyW))+"  "+styleMuted.Render(b.Help().Desc))
			}
		}
		return strings.Join(rows, "\n")
	}

	cols := helpGroups()
	parts := make([]string, 0, 2*len(cols)-1)
	for i, c := range cols {
		if i > 0 {
			parts = append(parts, "    ")
		}
		parts = append(parts, column(c))
	}
	body := styleAccent.Bold(true).Render("Keys") + "\n\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, parts...) + "\n\n" +
		styleMuted.Render("Esc or ? to close")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Render(body)
}
