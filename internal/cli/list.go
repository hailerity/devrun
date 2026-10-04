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
	printGatewayLine(res.Gateway)
	return nil
}

// printGatewayLine says where the table's URLs lead, and — the part that
// matters — whether anything can reach them from off this machine. A list of
// public addresses with no word about the posture would read as harmless.
func printGatewayLine(gw *ipc.GatewayStatusPayload) {
	if gw == nil || !gw.Running {
		return
	}
	fmt.Println()
	where := "http://" + config.DisplayHost(gw.Addr) + "/"
	if gw.Tunnel != nil && gw.Tunnel.Running && gw.Tunnel.PublicURL != "" {
		where = gw.Tunnel.PublicURL
	}
	posture := styleGreen.Render("local")
	if gw.Posture == config.PosturePublished {
		posture = styleYellow.Render("published")
	}
	fmt.Printf("%s  %s  %s\n", styleLabel.Render("gateway"), styleAccent.Render(where), posture)
}

var listHeaders = []string{"NAME", "GROUP", "STATE", "PID", "PORT", "UPTIME", "CPU%", "MEM"}

// listHeadersFor adds a URL column only when there is one to show. URLs are
// long, and a column of dashes would cost every local user width for nothing.
func listHeadersFor(svcs []ipc.ServiceInfo) []string {
	for _, s := range svcs {
		if s.URL != "" {
			return append(append([]string{}, listHeaders...), "URL")
		}
	}
	return listHeaders
}

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

// serviceRowCellsFor is serviceRowCells plus the URL column, when the table
// has one.
func serviceRowCellsFor(svc ipc.ServiceInfo, headers []string) []string {
	cells := serviceRowCells(svc)
	if len(headers) == len(listHeaders) {
		return cells
	}
	url := svc.URL
	if url == "" {
		url = "-"
	}
	return append(cells, url)
}

func printServiceTable(svcs []ipc.ServiceInfo) {
	const gap = "  "
	headers := listHeadersFor(svcs)

	// Collect raw cell values and compute column widths.
	rows := make([][]string, len(svcs))
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for i, svc := range svcs {
		rows[i] = serviceRowCellsFor(svc, headers)
		for j, cell := range rows[i] {
			if len(cell) > widths[j] {
				widths[j] = len(cell)
			}
		}
	}

	// Header row. The last column takes no trailing pad, as its data cells do
	// not — otherwise every header line ends in as much whitespace as the
	// widest value under it, which the URL column made plain.
	parts := make([]string, len(headers))
	for i, h := range headers {
		if i == len(headers)-1 {
			parts[i] = styleLabel.Render(h)
			continue
		}
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
			case len(row) - 1: // the last column takes no trailing pad
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
