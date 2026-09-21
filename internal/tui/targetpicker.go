package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/ipc"
)

// pickerRows caps how many target rows the picker shows at once (the window
// follows the cursor) and pickerMembers how many members it lists, so a large
// config cannot grow the modal past the terminal.
const (
	pickerRows    = 10
	pickerMembers = 8
)

// targetPicker is the modal that chooses which target filters the service list.
// Row 0 is always the synthetic "All services" entry (clear the filter); the
// real targets follow in the order the model built them. Like the edit modals
// it is a keyboard trap while open.
type targetPicker struct {
	open   bool
	cursor int // 0 = "All services", i>0 = targets[i-1]
}

// openAt shows the picker with the cursor on the currently active filter, so
// Enter straight away is a no-op rather than a surprise change.
func (p *targetPicker) openAt(targets []sidebarTarget, filter string) {
	p.open = true
	p.cursor = 0
	for i, t := range targets {
		if t.name == filter {
			p.cursor = i + 1
		}
	}
}

func (p *targetPicker) close() { p.open = false }

// move shifts the cursor by d, wrapping over the n targets plus the
// "All services" row.
func (p *targetPicker) move(d, n int) {
	rows := n + 1
	p.cursor = ((p.cursor+d)%rows + rows) % rows
}

// selected returns the highlighted target's name — "" for "All services" or a
// cursor that a shrinking target list left out of range.
func (p *targetPicker) selected(targets []sidebarTarget) string {
	if p.cursor < 1 || p.cursor > len(targets) {
		return ""
	}
	return targets[p.cursor-1].name
}

func (p targetPicker) view(targets []sidebarTarget, all []ipc.ServiceInfo, filter string, width, height int) string {
	state := make(map[string]string, len(all))
	running := 0
	for _, s := range all {
		state[s.Name] = s.State
		if s.State == "running" {
			running++
		}
	}
	countUp := func(members []string) int {
		n := 0
		for _, m := range members {
			if state[m] == "running" {
				n++
			}
		}
		return n
	}

	nameW := lipgloss.Width(allServicesLabel)
	for _, t := range targets {
		nameW = max(nameW, lipgloss.Width(t.name))
	}

	row := func(i int, name, label string, up, total int) string {
		marker := " "
		if name == filter {
			marker = "▸"
		}
		count := fmt.Sprintf("%d/%d", up, total)
		if total > 0 && up == total {
			count = styleGreen.Render(count)
		} else {
			count = styleMuted.Render(count)
		}
		line := fmt.Sprintf("%s %-*s  ", styleAccent.Render(marker), nameW, label)
		if i == p.cursor {
			line = styleAccent.Render(marker) + " " + styleText.Bold(true).Render(fmt.Sprintf("%-*s", nameW, label)) + "  "
		}
		return line + count
	}

	var b strings.Builder
	b.WriteString(styleAccent.Bold(true).Render("Filter by target") + "\n\n")
	// Row 0 is "All services"; rows 1..n are the targets. Show a window of
	// pickerRows rows that keeps the cursor in view.
	total := len(targets) + 1
	top := min(max(0, p.cursor-pickerRows+1), max(0, total-pickerRows))
	end := min(total, top+pickerRows)
	for i := top; i < end; i++ {
		if i == 0 {
			b.WriteString(row(0, "", allServicesLabel, running, len(all)) + "\n")
			continue
		}
		t := targets[i-1]
		b.WriteString(row(i, t.name, t.name, countUp(t.members), len(t.members)) + "\n")
	}
	if total > pickerRows {
		b.WriteString(styleMuted.Render(fmt.Sprintf("  %d–%d of %d", top+1, end, total)) + "\n")
	}

	// Members of the highlighted target, in declared order — the roll-up that
	// used to fill the main pane.
	if name := p.selected(targets); name != "" {
		b.WriteString("\n" + styleMuted.Render("members") + "\n")
		members := targets[p.cursor-1].members
		if len(members) == 0 {
			b.WriteString("  " + styleMuted.Render("(no services)") + "\n")
		}
		shown := members
		if len(shown) > pickerMembers {
			shown = shown[:pickerMembers]
		}
		for _, m := range shown {
			st, ok := state[m]
			if !ok {
				fmt.Fprintf(&b, "  %s %s  %s\n", styleMuted.Render("○"), m, styleMuted.Render("not reported"))
				continue
			}
			fmt.Fprintf(&b, "  %s %s\n", stateDot(st), m)
		}
		if extra := len(members) - len(shown); extra > 0 {
			b.WriteString("  " + styleMuted.Render(fmt.Sprintf("+%d more", extra)) + "\n")
		}
	}

	b.WriteString("\n" + styleMuted.Render("↵ filter · e edit · Esc close"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Render(b.String())
	if height < lipgloss.Height(box) {
		height = lipgloss.Height(box)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
