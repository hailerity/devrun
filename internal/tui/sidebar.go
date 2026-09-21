package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/ipc"
)

// sidebarTarget is one configured target: its name and its declared members.
// Targets are not sidebar rows — they feed the target picker and the service
// filter.
type sidebarTarget struct {
	name    string
	members []string
}

// allServicesLabel names the "no filter" choice in the target picker.
const allServicesLabel = "All services"

type sidebar struct {
	allServices []ipc.ServiceInfo // full scoped list: crashed first, then by Name
	services    []ipc.ServiceInfo // allServices filtered to the active target
	selected    int               // cursor within services
	top         int               // first visible services row — the scroll window's offset
	rows        int               // visible row count, set by the model's layout; 0 = not laid out yet

	targets      []sidebarTarget // configured targets, sorted; empty → nothing to filter by
	filterTarget string          // name of the target filtering the list ("" = show all); set via the target picker

	loaded bool // true once the first daemon poll has resolved (response or error)
}

func (s *sidebar) update(svcs []ipc.ServiceInfo, targets []sidebarTarget) {
	s.loaded = true

	var curSvc string
	if s.selected < len(s.services) {
		curSvc = s.services[s.selected].Name
	}

	// Crashed services lead the list so a failure is never below the fold;
	// everything else stays alphabetical. The cursor follows its service by
	// name (below), so a row that jumps to the top takes the highlight with it.
	sorted := append([]ipc.ServiceInfo(nil), svcs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ci, cj := sorted[i].State == "crashed", sorted[j].State == "crashed"
		if ci != cj {
			return ci
		}
		return sorted[i].Name < sorted[j].Name
	})
	s.allServices = sorted
	s.targets = targets

	// Keep the filter across the poll; drop it only if that target is gone.
	if s.filterTarget != "" && !s.targetExists(s.filterTarget) {
		s.filterTarget = ""
	}

	s.refilter()
	s.selectServiceByName(curSvc)
}

// selectServiceByName moves the service cursor to the row named n, or to row 0
// when there is no such row. Call after the filtered service list changes.
func (s *sidebar) selectServiceByName(n string) {
	s.selected = 0
	for i, svc := range s.services {
		if svc.Name == n {
			s.selected = i
			break
		}
	}
	s.scrollToCursor()
}

// target returns the configured target called name, or nil.
func (s *sidebar) target(name string) *sidebarTarget {
	for i := range s.targets {
		if s.targets[i].name == name {
			return &s.targets[i]
		}
	}
	return nil
}

// targetExists reports whether a target with the given name is configured.
func (s *sidebar) targetExists(name string) bool { return s.target(name) != nil }

// refilter recomputes s.services from s.allServices and the active target
// filter, then clamps the service cursor into range.
func (s *sidebar) refilter() {
	if t := s.target(s.filterTarget); s.filterTarget == "" || t == nil {
		s.services = s.allServices
	} else {
		members := make(map[string]bool, len(t.members))
		for _, m := range t.members {
			members[m] = true
		}
		out := make([]ipc.ServiceInfo, 0, len(s.allServices))
		for _, svc := range s.allServices {
			if members[svc.Name] {
				out = append(out, svc)
			}
		}
		s.services = out
	}
	if s.selected >= len(s.services) {
		s.selected = max(0, len(s.services)-1)
	}
}

// setFilter makes the target called name the service filter ("" or an unknown
// name clears it), keeping the highlight on the same service if it survived.
func (s *sidebar) setFilter(name string) {
	var curSvc string
	if s.selected < len(s.services) {
		curSvc = s.services[s.selected].Name
	}
	if !s.targetExists(name) {
		name = ""
	}
	s.filterTarget = name
	s.refilter()
	s.selectServiceByName(curSvc)
}

// setRows tells the sidebar how many rows its pane can show, and re-anchors the
// scroll window on the cursor.
func (s *sidebar) setRows(n int) {
	s.rows = n
	s.scrollToCursor()
}

// scrollToCursor moves the window the least it must to keep the cursor visible,
// and never leaves blank rows below the list when there is more above. Called
// after anything that moves the cursor or changes the list.
func (s *sidebar) scrollToCursor() {
	if s.rows <= 0 {
		s.top = 0 // not laid out yet: render() shows everything
		return
	}
	if s.selected < s.top {
		s.top = s.selected
	}
	if s.selected >= s.top+s.rows {
		s.top = s.selected - s.rows + 1
	}
	s.top = max(0, min(s.top, len(s.services)-s.rows))
}

// moveDown / moveUp walk the (filtered) service list, wrapping at the ends.

func (s *sidebar) moveDown() {
	if len(s.services) == 0 {
		return
	}
	s.selected = (s.selected + 1) % len(s.services)
	s.scrollToCursor()
}

func (s *sidebar) moveUp() {
	if len(s.services) == 0 {
		return
	}
	s.selected = (s.selected - 1 + len(s.services)) % len(s.services)
	s.scrollToCursor()
}

func (s *sidebar) selectedService() *ipc.ServiceInfo {
	if len(s.services) == 0 {
		return nil
	}
	return &s.services[s.selected]
}

// stateLabel returns the short status token for a service: its port when
// running, otherwise the state word.
func stateLabel(svc ipc.ServiceInfo) string {
	if svc.State == "running" {
		if svc.Port != nil && *svc.Port != 0 {
			return fmt.Sprintf(":%d", *svc.Port)
		}
		return "detecting"
	}
	return svc.State
}

// truncateName shortens s to fit w display columns, keeping the head and the
// tail and marking the cut with "…" in the middle — so a shared prefix and the
// distinguishing suffix both stay visible.
func truncateName(s string, w int) string {
	if w < 1 {
		w = 1
	}
	total := lipgloss.Width(s)
	if total <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	// Cut by display columns, not runes: a CJK or emoji name is two columns per
	// rune, and a rune-count cut would hand back something wider than w. Both
	// ends are measured rune by rune rather than left to a library's handling of
	// a cut that lands inside a wide rune (ansi.TruncateLeft keeps the whole
	// rune, which overshoots by a column) — so the result can come back a column
	// under w, never over.
	keep := w - 1 // room taken by the ellipsis
	headW := (keep + 1) / 2
	tailW := keep - headW

	r := []rune(s)
	head, used := 0, 0
	for head < len(r) {
		rw := lipgloss.Width(string(r[head]))
		if used+rw > headW {
			break
		}
		used += rw
		head++
	}
	tail, used := len(r), 0
	for tail > head {
		rw := lipgloss.Width(string(r[tail-1]))
		if used+rw > tailW {
			break
		}
		used += rw
		tail--
	}
	return string(r[:head]) + "…" + string(r[tail:])
}

// padRight pads s with spaces to w display columns. fmt's %-*s counts runes,
// which under-pads nothing but over-runs on wide characters — this counts what
// the terminal will actually draw.
func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// stateGlyph returns the marker and colour for a service state. The shape alone
// identifies the state — ● running, ◐ in transition (starting / stopping),
// ✖ crashed, ○ not running — so it still reads under --no-color or for a
// colour-blind user; the colour only reinforces it.
func stateGlyph(state string) (string, lipgloss.TerminalColor) {
	switch state {
	case "running":
		return "●", colorGreen
	case "starting", "stopping":
		return "◐", colorYellow
	case "crashed":
		return "✖", colorRed
	default:
		return "○", colorMuted
	}
}

func stateDot(state string) string {
	glyph, fg := stateGlyph(state)
	return lipgloss.NewStyle().Foreground(fg).Render(glyph)
}

// frame is the sidebar's border: the title names the list and any target
// filtering it — so the reason a service is missing is always on screen — and
// the bottom edge counts how many of the listed services are up.
func (s *sidebar) frame(focused bool) paneFrame {
	title := styleMuted.Render("SERVICES")
	if focused {
		title = styleAccent.Bold(true).Render("SERVICES")
	}
	if s.filterTarget != "" {
		title += styleMuted.Render(" · ") + styleAccent.Render(s.filterTarget)
	}
	f := paneFrame{title: title, focused: focused}
	if len(s.services) > 0 {
		up := 0
		for _, svc := range s.services {
			if svc.State == "running" {
				up++
			}
		}
		f.footLeft = styleMuted.Render(fmt.Sprintf("%d/%d up", up, len(s.services)))
		// Say so when the list is windowed — otherwise rows above or below the
		// fold are invisible with nothing to hint they exist.
		if first, last := s.window(); last-first < len(s.services) {
			f.footRight = styleMuted.Render(fmt.Sprintf("%d–%d of %d", first+1, last, len(s.services)))
		}
	}
	return f
}

// render draws the visible window of list rows for a content area `width`
// columns wide.
func (s *sidebar) render(width int) string {
	switch {
	case len(s.allServices) == 0 && !s.loaded:
		return styleMuted.Render(" Loading services…")
	case len(s.allServices) == 0:
		return styleMuted.Render(" No services — run devrun add <name>")
	case len(s.services) == 0:
		return styleMuted.Render(" (no services in target)")
	}
	first, last := s.window()
	rows := make([]string, 0, last-first)
	for i := first; i < last; i++ {
		rows = append(rows, serviceRow(width, s.services[i], i == s.selected))
	}
	return strings.Join(rows, "\n")
}

// window returns the half-open range of service rows currently visible.
func (s *sidebar) window() (first, last int) {
	if s.rows <= 0 {
		return 0, len(s.services)
	}
	first = max(0, min(s.top, len(s.services)))
	return first, min(len(s.services), first+s.rows)
}

// Column widths of a service row: " ● name  :8080   2.1%".
const (
	rowStateW = 9 // "detecting" / "stopping" — the longest state token
	rowCPUW   = 6 // "100.0%"
	// Below these row widths the CPU column, then the state column, is dropped
	// so the name keeps a usable share of a narrow sidebar.
	rowMinWForCPU   = 27
	rowMinWForState = 19
)

// serviceRow renders one table row of the service list — glyph, name, port or
// state, CPU — exactly `width` columns wide. Every segment of a selected row
// carries the selection background itself, so an SGR reset inside one styled
// segment cannot punch a hole in the highlight.
func serviceRow(width int, svc ipc.ServiceInfo, selected bool) string {
	base := lipgloss.NewStyle()
	if selected {
		base = base.Background(colorSelSidebar)
	}
	showState := width >= rowMinWForState
	showCPU := width >= rowMinWForCPU

	nameW := width - 3 // margin + glyph + space
	if showState {
		nameW -= 1 + rowStateW
	}
	if showCPU {
		nameW -= 1 + rowCPUW
	}
	nameW = max(1, nameW)

	glyph, glyphFg := stateGlyph(svc.State)
	row := base.Foreground(glyphFg).Render(" "+glyph) +
		base.Foreground(colorText).Render(" "+padRight(truncateName(svc.Name, nameW), nameW))

	if showState {
		// A running service shows where to reach it; any other state is named,
		// in the state's own colour.
		label, fg := stateLabel(svc), glyphFg
		switch {
		case svc.State == "running" && strings.HasPrefix(label, ":"):
			fg = colorAccent
		case svc.State == "running":
			fg = colorMuted
		}
		row += base.Foreground(fg).Render(" " + fmt.Sprintf("%-*s", rowStateW, label))
	}
	if showCPU {
		cpu := ""
		if svc.State == "running" {
			cpu = fmt.Sprintf("%.1f%%", svc.CPUPct)
		}
		row += base.Foreground(cpuColor(svc.CPUPct)).Render(" " + fmt.Sprintf("%*s", rowCPUW, cpu))
	}
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += base.Render(strings.Repeat(" ", pad))
	}
	return row
}

// cpuColor keeps an idle CPU figure quiet: only a busy service is coloured, so
// yellow and red mean something when they appear.
func cpuColor(pct float64) lipgloss.TerminalColor {
	switch {
	case pct > 80:
		return colorRed
	case pct > 50:
		return colorYellow
	default:
		return colorMuted
	}
}
