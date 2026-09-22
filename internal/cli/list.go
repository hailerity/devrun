package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/hailerity/devrun/internal/ops"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all services with status",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

func runList(_ *cobra.Command, _ []string) error {
	// activeRegistry already scopes to the local devrun.yaml, or — for the global
	// registry — to services the user registered directly (project mirrors hidden).
	reg, _, err := activeRegistry()
	if err != nil {
		return err
	}
	res, err := ops.List(&ops.Resolved{Registry: reg})
	if err != nil {
		return err
	}
	if res.Offline {
		fmt.Fprintln(os.Stderr, "(daemon not running — showing last known state)")
	}
	printServiceTable(res.Services)
	return nil
}

var listHeaders = []string{"NAME", "GROUP", "STATE", "PID", "PORT", "UPTIME", "CPU%", "MEM"}

func serviceRowCells(svc ipc.ServiceInfo) []string {
	pid := "-"
	if svc.PID != nil {
		pid = fmt.Sprintf("%d", *svc.PID)
	}
	port := "-"
	if svc.Port != nil {
		port = fmt.Sprintf(":%d", *svc.Port)
	}
	uptime := "-"
	if svc.UptimeSec > 0 {
		uptime = formatUptime(svc.UptimeSec)
	}
	cpu, mem := "-", "-"
	if svc.CPUPct > 0 || svc.MemBytes > 0 {
		cpu = fmt.Sprintf("%.1f%%", svc.CPUPct)
		mem = formatBytes(svc.MemBytes)
	}
	group := svc.Group
	if group == "" {
		group = "-"
	}
	return []string{svc.Name, group, svc.State, pid, port, uptime, cpu, mem}
}

func printServiceTable(svcs []ipc.ServiceInfo) {
	const gap = "  "

	// Collect raw cell values and compute column widths.
	rows := make([][]string, len(svcs))
	widths := make([]int, len(listHeaders))
	for i, h := range listHeaders {
		widths[i] = len(h)
	}
	for i, svc := range svcs {
		rows[i] = serviceRowCells(svc)
		for j, cell := range rows[i] {
			if len(cell) > widths[j] {
				widths[j] = len(cell)
			}
		}
	}

	// Header row.
	parts := make([]string, len(listHeaders))
	for i, h := range listHeaders {
		parts[i] = styleLabel.Render(fmt.Sprintf("%-*s", widths[i], h))
	}
	fmt.Fprintln(os.Stdout, strings.Join(parts, gap))

	// Data rows.
	for ri, row := range rows {
		for i, cell := range row {
			pad := widths[i]
			switch i {
			case 0: // NAME
				parts[i] = styleBold.Render(fmt.Sprintf("%-*s", pad, cell))
			case 2: // STATE
				parts[i] = stateStyle(svcs[ri].State).Render(fmt.Sprintf("%-*s", pad, cell))
			case len(row) - 1: // MEM — last column, no trailing pad
				if cell == "-" {
					parts[i] = styleLabel.Render(cell)
				} else {
					parts[i] = styleValue.Render(cell)
				}
			default:
				if cell == "-" {
					parts[i] = styleLabel.Render(fmt.Sprintf("%-*s", pad, cell))
				} else {
					parts[i] = styleValue.Render(fmt.Sprintf("%-*s", pad, cell))
				}
			}
		}
		fmt.Fprintln(os.Stdout, strings.Join(parts, gap))
	}
}

func stateStyle(state string) lipgloss.Style {
	switch config.ServiceStatus(state) {
	case config.StatusRunning:
		return styleGreen
	case config.StatusCrashed:
		return styleRed
	default:
		return styleLabel
	}
}

func formatUptime(sec int64) string {
	d := time.Duration(sec) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatBytes(b int64) string {
	const mb = 1024 * 1024
	if b >= mb {
		return fmt.Sprintf("%.0fM", float64(b)/float64(mb))
	}
	return fmt.Sprintf("%dK", b/1024)
}
