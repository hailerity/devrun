package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/ipc"
)

type sidebar struct {
	services []ipc.ServiceInfo // always sorted by Name
	selected int
}

func (s *sidebar) update(svcs []ipc.ServiceInfo) {
	// Remember current name before replacing
	var cur string
	if s.selected < len(s.services) {
		cur = s.services[s.selected].Name
	}

	sorted := make([]ipc.ServiceInfo, len(svcs))
	copy(sorted, svcs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	s.services = sorted

	// Restore by name, fallback to 0
	s.selected = 0
	for i, svc := range s.services {
		if svc.Name == cur {
			s.selected = i
			break
		}
	}
}

func (s *sidebar) moveUp() {
	if len(s.services) == 0 {
		return
	}
	if s.selected == 0 {
		s.selected = len(s.services) - 1
	} else {
		s.selected--
	}
}

func (s *sidebar) moveDown() {
	if len(s.services) == 0 {
		return
	}
	if s.selected == len(s.services)-1 {
		s.selected = 0
	} else {
		s.selected++
	}
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

func (s *sidebar) render(width, height int, focused bool) string {
	if len(s.services) == 0 {
		return styleMuted.Render("No services — run devrun add <name>")
	}

	// --- Top: header + service list ---

	// Header — styled and bordered to align visually with the main tab bar.
	svcLabel := styleMuted.Render("SERVICES")
	if focused {
		svcLabel = styleAccent.Underline(true).Render("SERVICES")
	}
	header := lipgloss.NewStyle().
		Width(width).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Render(svcLabel)
	top := []string{header}

	// Rows show only the status dot and the name — the name gets the full row
	// width. Port/state live in the info block pinned to the bottom.
	for i, svc := range s.services {
		name := truncateName(svc.Name, width-3) // dot(1) + space(1) + margin(1)

		if i == s.selected {
			top = append(top, selectedServiceRow(width, svc.State, name))
		} else {
			top = append(top, stateDot(svc.State)+" "+name)
		}
	}

	// --- Bottom: selected-service info block + action hints, pinned to the
	// bottom edge so their position doesn't drift with the service count. ---

	var bottom []string
	if svc := s.selectedService(); svc != nil {
		sep := "── " + truncateName(svc.Name, width-6) + " ──"
		bottom = append(bottom,
			styleMuted.Render(sep),
			"  "+stateLine(*svc),
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

func renderCPUPct(pct float64) string {
	s := fmt.Sprintf("%.1f%%", pct)
	if pct > 80 {
		return styleRed.Render(s)
	}
	return styleYellow.Render(s)
}
