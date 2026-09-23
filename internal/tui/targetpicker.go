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
//
// Both regions are rendered at exactly those heights whatever the cursor is on —
// short lists are padded rather than collapsed. The modal is centred by overlay()
// on every frame, so a box that grew or shrank with the selection would jump
// under the cursor as you moved through the targets.
const (
	pickerRows    = 10
	pickerMembers = 8

	// pickerNameMax bounds the name column so one very long service name cannot
	// push the modal wider than the terminal.
	pickerNameMax = 28

	pickerTitle      = "Filter by target"
	pickerHints      = "↵ filter · e edit · Esc close"
	notReportedLabel = "not reported"
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

func (p targetPicker) view(targets []sidebarTarget, all []ipc.ServiceInfo, filter string) string {
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
	nameW = min(nameW, pickerNameMax)

	// contentW is measured over every row and every member list the modal could
	// draw, not just the ones the current cursor reveals — otherwise the box
	// would also change width as the selection moved.
	countW := lipgloss.Width(fmt.Sprintf("%d/%d", running, len(all)))
	for _, t := range targets {
		countW = max(countW, lipgloss.Width(fmt.Sprintf("%d/%d", countUp(t.members), len(t.members))))
	}
	contentW := 3 + nameW + 2 + countW // gutter + marker + space, name, gap, count

	allNames := make([]string, 0, len(all))
	for _, s := range all {
		allNames = append(allNames, s.Name)
	}
	// memberNameW is one column width for every member list, so the "not reported"
	// note lines up down the region instead of sitting wherever each name ends.
	memberNameW := 0
	// memberRows and overflowRow are measured over every list the modal could
	// show, so the region is the same height for all of them — but only as tall as
	// the config actually needs, rather than always the full cap.
	memberRows, overflowRow := 1, false
	eachList := func(fn func([]string)) {
		fn(allNames)
		for _, t := range targets {
			fn(t.members)
		}
	}
	eachList(func(names []string) {
		memberRows = max(memberRows, min(len(names), pickerMembers))
		overflowRow = overflowRow || len(names) > pickerMembers
		for _, n := range names {
			memberNameW = max(memberNameW, lipgloss.Width(truncateName(n, pickerNameMax)))
		}
	})
	eachList(func(names []string) {
		for _, n := range names {
			w := 4 + memberNameW // indent + dot + space
			if _, ok := state[n]; !ok {
				w += 2 + lipgloss.Width(notReportedLabel)
			}
			contentW = max(contentW, w)
		}
	})
	for _, s := range []string{pickerTitle, pickerHints} {
		contentW = max(contentW, lipgloss.Width(s))
	}

	// pad brings a rendered line up to contentW so every row of the box is the
	// same width — a short line would let lipgloss size the border to whatever
	// the current selection happens to draw.
	pad := func(s string) string {
		return s + strings.Repeat(" ", max(0, contentW-lipgloss.Width(s)))
	}

	// row renders one target row at exactly contentW columns. Every segment is
	// built from base so it carries the selection background itself: an SGR reset
	// inside a styled segment cannot then punch a hole in the highlight, the same
	// rule the sidebar's service rows follow.
	row := func(i int, name, label string, up, total int) string {
		base := lipgloss.NewStyle()
		gutter := " "
		if i == p.cursor {
			base = base.Background(colorSelSidebar)
			gutter = "▌"
		}
		marker := " "
		if name == filter {
			marker = "▸"
		}
		nameStyle := base.Foreground(colorText)
		if i == p.cursor {
			nameStyle = nameStyle.Bold(true)
		}
		countStyle := base.Foreground(colorMuted)
		if total > 0 && up == total {
			countStyle = base.Foreground(colorGreen)
		}
		line := base.Foreground(colorAccent).Render(gutter+marker) +
			base.Render(" ") +
			nameStyle.Render(padRight(truncateName(label, nameW), nameW)) +
			base.Render("  ") +
			countStyle.Render(fmt.Sprintf("%d/%d", up, total))
		return line + base.Render(strings.Repeat(" ", max(0, contentW-lipgloss.Width(line))))
	}

	var b strings.Builder
	b.WriteString(pad(styleAccent.Bold(true).Render(pickerTitle)) + "\n\n")
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
		b.WriteString(pad(styleMuted.Render(fmt.Sprintf("  %d–%d of %d", top+1, end, total))) + "\n")
	}

	// The roll-up under the list: the highlighted target's members in declared
	// order, or every service when the cursor is on "All services" — so the
	// region says what the filter would select either way, and is never absent.
	label, members := "services", allNames
	if name := p.selected(targets); name != "" {
		label, members = "members", targets[p.cursor-1].members
	}
	b.WriteString("\n" + pad(styleMuted.Render(label)) + "\n")

	shown := members
	if len(shown) > pickerMembers {
		shown = shown[:pickerMembers]
	}
	drawn := 0
	if len(members) == 0 {
		b.WriteString(pad("  "+styleMuted.Render("(no services)")) + "\n")
		drawn++
	}
	for _, m := range shown {
		name := padRight(truncateName(m, pickerNameMax), memberNameW)
		if st, ok := state[m]; ok {
			b.WriteString(pad(fmt.Sprintf("  %s %s", stateDot(st), name)) + "\n")
		} else {
			b.WriteString(pad(fmt.Sprintf("  %s %s  %s", styleMuted.Render("○"), name, styleMuted.Render(notReportedLabel))) + "\n")
		}
		drawn++
	}
	for ; drawn < memberRows; drawn++ {
		b.WriteString(pad("") + "\n")
	}
	// Once any list overruns the cap the row is reserved for all of them, so the
	// one that overflows and the one that does not come out the same height.
	if overflowRow {
		line := ""
		if extra := len(members) - len(shown); extra > 0 {
			line = "  " + styleMuted.Render(fmt.Sprintf("+%d more", extra))
		}
		b.WriteString(pad(line) + "\n")
	}

	b.WriteString("\n" + pad(styleMuted.Render(pickerHints)))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Render(b.String())
}
