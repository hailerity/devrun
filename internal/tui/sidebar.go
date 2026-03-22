package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/procet/internal/ipc"
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

// stateLabel returns the right-side label for a service row.
func stateLabel(svc ipc.ServiceInfo) string {
	if svc.State == "running" {
		if svc.Port != nil && *svc.Port != 0 {
			return fmt.Sprintf(":%d", *svc.Port)
		}
		return "detecting"
	}
	return svc.State
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
		return styleMuted.Render("No services — run procet add <name>")
	}

	var rows []string

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
	rows = append(rows, header)

	for i, svc := range s.services {
		label := stateLabel(svc)
		nameW := width - 4 - lipgloss.Width(label)
		if nameW < 1 {
			nameW = 1
		}
		name := svc.Name
		if len(name) > nameW {
			name = name[:nameW]
		}

		var line string
		if i == s.selected {
			line = selectedServiceRow(width, svc.State, name, nameW, label)
		} else {
			dot := stateDot(svc.State)
			line = dot + " " + fmt.Sprintf("%-*s", nameW, name) + styleMuted.Render(label)
		}
		rows = append(rows, line)
	}

	// Mini stats for selected service (blank line for visual separation).
	if svc := s.selectedService(); svc != nil {
		rows = append(rows, "")
		sep := fmt.Sprintf("── %s ──", svc.Name)
		rows = append(rows, styleMuted.Render(sep))
		rows = append(rows, fmt.Sprintf("CPU  %s", renderCPUPct(svc.CPUPct)))
		rows = append(rows, fmt.Sprintf("MEM  %s", formatBytes(svc.MemBytes)))
		rows = append(rows, fmt.Sprintf("UP   %s", formatUptime(svc.UptimeSec)))
	}

	// Action hints at bottom
	rows = append(rows, strings.Repeat("─", width))
	rows = append(rows, renderHint("s", "start"))
	rows = append(rows, renderHint("x", "stop"))

	return strings.Join(rows, "\n")
}

// selectedServiceRow builds a full-width highlighted row for the selected
// service. Each segment explicitly carries the selection background so that
// internal SGR resets from sub-styles do not clear it mid-line.
func selectedServiceRow(width int, state, name string, nameW int, label string) string {
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
	namePart := sel.Foreground(colorText).Render(fmt.Sprintf(" %-*s", nameW, name))
	lbl := sel.Foreground(colorMuted).Render(label)
	content := dot + namePart + lbl

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
