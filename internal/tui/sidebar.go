package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/ipc"
)

// sidebarTarget is one row of the TARGETS list. The row at index 0 is the
// synthetic "All services" entry, distinguished by an empty name.
type sidebarTarget struct {
	name    string
	members []string
	active  bool
}

// allServicesLabel is the rendered text of the synthetic clear-filter row
// (sidebarTarget{name: ""}).
const allServicesLabel = "All services"

type sidebarSection int

const (
	sectionServices sidebarSection = iota
	sectionTargets
)

type sidebar struct {
	allServices []ipc.ServiceInfo // full scoped list, sorted by Name
	services    []ipc.ServiceInfo // allServices filtered to the active target
	selected    int               // cursor within services

	targets      []sidebarTarget // index 0 is "All services"; len <= 1 → no TARGETS block
	targetSel    int             // cursor within targets
	section      sidebarSection  // which list the cursor is in
	filterTarget string          // name of the target filtering services ("" = all)
}

// hasTargets reports whether there is at least one real target beyond the
// synthetic "All services" row — i.e. whether the TARGETS block is shown.
func (s *sidebar) hasTargets() bool { return len(s.targets) > 1 }

func (s *sidebar) update(svcs []ipc.ServiceInfo, targets []sidebarTarget) {
	var curSvc string
	if s.selected < len(s.services) {
		curSvc = s.services[s.selected].Name
	}
	var curTarget string
	if s.targetSel < len(s.targets) {
		curTarget = s.targets[s.targetSel].name
	}

	sorted := append([]ipc.ServiceInfo(nil), svcs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	s.allServices = sorted
	s.targets = targets

	if !s.hasTargets() {
		s.section = sectionServices
		s.targetSel = 0
		s.filterTarget = ""
	} else {
		s.targetSel = 0
		for i, t := range s.targets {
			if t.name == curTarget {
				s.targetSel = i
				break
			}
		}
		s.filterTarget = s.targets[s.targetSel].name
	}

	s.refilter()

	s.selected = 0
	for i, svc := range s.services {
		if svc.Name == curSvc {
			s.selected = i
			break
		}
	}
}

// refilter recomputes s.services from s.allServices and the active target
// filter, then clamps the service cursor into range.
func (s *sidebar) refilter() {
	if s.filterTarget == "" {
		s.services = s.allServices
	} else {
		var members map[string]bool
		for _, t := range s.targets {
			if t.name == s.filterTarget {
				members = make(map[string]bool, len(t.members))
				for _, m := range t.members {
					members[m] = true
				}
				break
			}
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

// onTargetCursorMoved re-points the service filter at the newly selected target
// row and re-filters the service list.
func (s *sidebar) onTargetCursorMoved() {
	if s.targetSel >= 0 && s.targetSel < len(s.targets) {
		s.filterTarget = s.targets[s.targetSel].name
	}
	s.refilter()
}

// moveDown / moveUp walk a single circular cursor over the TARGETS rows followed
// by the (filtered) SERVICES rows. With no targets, they wrap within services —
// identical to the pre-targets behaviour.

func (s *sidebar) moveDown() {
	if !s.hasTargets() {
		if len(s.services) == 0 {
			return
		}
		if s.selected == len(s.services)-1 {
			s.selected = 0
		} else {
			s.selected++
		}
		return
	}
	switch s.section {
	case sectionTargets:
		if s.targetSel < len(s.targets)-1 {
			s.targetSel++
			s.onTargetCursorMoved()
		} else {
			s.section = sectionServices
			s.selected = 0
		}
	case sectionServices:
		if len(s.services) > 0 && s.selected < len(s.services)-1 {
			s.selected++
		} else {
			s.section = sectionTargets
			s.targetSel = 0
			s.onTargetCursorMoved()
		}
	}
}

func (s *sidebar) moveUp() {
	if !s.hasTargets() {
		if len(s.services) == 0 {
			return
		}
		if s.selected == 0 {
			s.selected = len(s.services) - 1
		} else {
			s.selected--
		}
		return
	}
	switch s.section {
	case sectionServices:
		if s.selected > 0 {
			s.selected--
		} else {
			s.section = sectionTargets
			s.targetSel = len(s.targets) - 1
			s.onTargetCursorMoved()
		}
	case sectionTargets:
		if s.targetSel > 0 {
			s.targetSel--
			s.onTargetCursorMoved()
		} else {
			s.section = sectionServices
			s.selected = max(0, len(s.services)-1)
		}
	}
}

func (s *sidebar) selectedService() *ipc.ServiceInfo {
	if len(s.services) == 0 {
		return nil
	}
	return &s.services[s.selected]
}

// selectedTarget returns the highlighted target row, or nil when the cursor is
// in the services section, there are no targets, or the row is out of range.
// The synthetic "All services" row is returned like any other (name == "").
func (s *sidebar) selectedTarget() *sidebarTarget {
	if !s.hasTargets() || s.section != sectionTargets {
		return nil
	}
	if s.targetSel < 0 || s.targetSel >= len(s.targets) {
		return nil
	}
	return &s.targets[s.targetSel]
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

func stateDot(state string) string {
	switch state {
	case "running":
		return styleGreen.Render("●")
	case "crashed":
		return styleRed.Render("●")
	default:
		return styleMuted.Render("●")
	}
}

func targetDot(t sidebarTarget) string {
	if t.active {
		return styleGreen.Render("●")
	}
	return styleMuted.Render("○")
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
	if len(s.allServices) == 0 && !s.hasTargets() {
		return styleMuted.Render("No services — run devrun add <name>")
	}

	var top []string

	// --- TARGETS block (only when a real target exists) ---
	if s.hasTargets() {
		top = append(top, sectionHeader("TARGETS", width, focused && s.section == sectionTargets))
		for i, t := range s.targets {
			label := t.name
			if label == "" {
				label = allServicesLabel
			}
			label = truncateName(label, width-3)
			if s.section == sectionTargets && i == s.targetSel {
				top = append(top, selectedTargetRow(width, t, label))
			} else {
				top = append(top, targetDot(t)+" "+label)
			}
		}
		top = append(top, "")
	}

	// --- SERVICES block ---
	servicesActive := !s.hasTargets() || s.section == sectionServices
	top = append(top, sectionHeader("SERVICES", width, focused && servicesActive))

	if len(s.services) == 0 {
		top = append(top, styleMuted.Render("  (no services in target)"))
	}
	for i, svc := range s.services {
		name := truncateName(svc.Name, width-3) // dot(1) + space(1) + margin(1)
		if servicesActive && i == s.selected {
			top = append(top, selectedServiceRow(width, svc.State, name))
		} else {
			top = append(top, stateDot(svc.State)+" "+name)
		}
	}

	// --- Bottom: info block for the selected row + action hints, pinned to the
	// bottom edge so their position doesn't drift with the list length. ---

	var bottom []string
	if t := s.selectedTarget(); t != nil && t.name != "" {
		state := styleMuted.Render("stopped")
		if t.active {
			state = styleGreen.Render("running")
		}
		bottom = append(bottom,
			styleMuted.Render("── "+truncateName(t.name, width-6)+" ──"),
			"  "+state,
			fmt.Sprintf("SVCS %d", len(t.members)),
		)
	} else if svc := s.selectedService(); svc != nil {
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

	var dotFg lipgloss.Color
	switch state {
	case "running":
		dotFg = colorGreen
	case "crashed":
		dotFg = colorRed
	default:
		dotFg = colorMuted
	}

	dot := sel.Foreground(dotFg).Render("●")
	namePart := sel.Foreground(colorText).Render(" " + name)
	content := dot + namePart

	// Fill remaining columns with the selection background.
	if pad := width - lipgloss.Width(content); pad > 0 {
		content += sel.Render(strings.Repeat(" ", pad))
	}
	return content
}

// selectedTargetRow is selectedServiceRow's equivalent for a TARGETS row: the
// dot is green for an active target, muted otherwise.
func selectedTargetRow(width int, t sidebarTarget, label string) string {
	sel := lipgloss.NewStyle().Background(colorSelSidebar)

	dotFg := colorMuted
	glyph := "○"
	if t.active {
		dotFg = colorGreen
		glyph = "●"
	}
	dot := sel.Foreground(dotFg).Render(glyph)
	namePart := sel.Foreground(colorText).Render(" " + label)
	content := dot + namePart
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
