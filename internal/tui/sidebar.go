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
	allServices []ipc.ServiceInfo // full scoped list, sorted by Name
	services    []ipc.ServiceInfo // allServices filtered to the active target
	selected    int               // cursor within services

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

	sorted := append([]ipc.ServiceInfo(nil), svcs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
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
			return
		}
	}
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

// moveDown / moveUp walk the (filtered) service list, wrapping at the ends.

func (s *sidebar) moveDown() {
	if len(s.services) == 0 {
		return
	}
	s.selected = (s.selected + 1) % len(s.services)
}

func (s *sidebar) moveUp() {
	if len(s.services) == 0 {
		return
	}
	s.selected = (s.selected - 1 + len(s.services)) % len(s.services)
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

// stateLine is the coloured status shown in the selected-service info block,
// e.g. "running :8080", "detecting", "stopped", "crashed".
func stateLine(svc ipc.ServiceInfo) string {
	switch svc.State {
	case "running":
		return styleGreen.Render("running ") + styleMuted.Render(stateLabel(svc))
	case "crashed":
		return styleRed.Render("crashed")
	default:
		return styleMuted.Render(svc.State)
	}
}

// truncateName shortens s to fit w display columns, keeping the head and the
// tail and marking the cut with "…" in the middle — so a shared prefix and the
// distinguishing suffix both stay visible.
func truncateName(s string, w int) string {
	if w < 1 {
		w = 1
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	r := []rune(s)
	keep := w - 1 // room taken by the ellipsis
	head := (keep + 1) / 2
	tail := keep - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
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

// sectionHeader renders a bordered sidebar column heading, accented while the
// cursor is in that section.
func sectionHeader(label string, width int, accented bool) string {
	txt := styleMuted.Render(label)
	if accented {
		txt = styleAccent.Underline(true).Render(label)
	}
	return lipgloss.NewStyle().
		Width(width).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Render(txt)
}

func (s *sidebar) render(width, height int, focused bool) string {
	if len(s.allServices) == 0 {
		if !s.loaded {
			return styleMuted.Render("Loading services…")
		}
		return styleMuted.Render("No services — run devrun add <name>")
	}

	// The heading names the active target filter, so the reason a service is
	// missing from the list is always on screen.
	heading := "SERVICES"
	if s.filterTarget != "" {
		heading += " · " + truncateName(s.filterTarget, max(1, width-len(heading)-3))
	}
	top := []string{sectionHeader(heading, width, focused)}

	if len(s.services) == 0 {
		top = append(top, styleMuted.Render("  (no services in target)"))
	}
	for i, svc := range s.services {
		name := truncateName(svc.Name, width-3) // dot(1) + space(1) + margin(1)
		if i == s.selected {
			top = append(top, selectedServiceRow(width, svc.State, name))
		} else {
			top = append(top, stateDot(svc.State)+" "+name)
		}
	}

	// --- Bottom: info block for the selected service + action hints, pinned to
	// the bottom edge so their position doesn't drift with the list length. ---

	var bottom []string
	if svc := s.selectedService(); svc != nil {
		sep := "── " + truncateName(svc.Name, width-6) + " ──"
		bottom = append(bottom,
			styleMuted.Render(sep),
			"  "+stateLine(*svc),
			fmt.Sprintf("PID  %s", renderPID(svc.PID)),
			fmt.Sprintf("CPU  %s", renderCPUPct(svc.CPUPct)),
			fmt.Sprintf("MEM  %s", formatBytes(svc.MemBytes)),
			fmt.Sprintf("UP   %s", formatUptime(svc.UptimeSec)),
		)
	}
	bottom = append(bottom,
		strings.Repeat("─", width),
		renderHint("s", "start"),
		renderHint("x", "stop"),
	)

	topStr := strings.Join(top, "\n")
	bottomStr := strings.Join(bottom, "\n")

	// Fill the space between the list and the bottom block. Clamped to one line
	// so an over-long list still renders (it overflows past the bottom edge,
	// the same as before — the sidebar has no scroll yet).
	gap := height - lipgloss.Height(topStr) - lipgloss.Height(bottomStr)
	if gap < 1 {
		gap = 1
	}
	return topStr + strings.Repeat("\n", gap+1) + bottomStr
}

// selectedServiceRow builds a full-width highlighted row for the selected
// service. Each segment explicitly carries the selection background so that
// internal SGR resets from sub-styles do not clear it mid-line.
func selectedServiceRow(width int, state, name string) string {
	sel := lipgloss.NewStyle().Background(colorSelSidebar)

	glyph, dotFg := stateGlyph(state)
	dot := sel.Foreground(dotFg).Render(glyph)
	namePart := sel.Foreground(colorText).Render(" " + name)
	content := dot + namePart

	// Fill remaining columns with the selection background.
	if pad := width - lipgloss.Width(content); pad > 0 {
		content += sel.Render(strings.Repeat(" ", pad))
	}
	return content
}

func renderCPUPct(pct float64) string {
	s := fmt.Sprintf("%.1f%%", pct)
	if pct > 80 {
		return styleRed.Render(s)
	}
	return styleYellow.Render(s)
}
