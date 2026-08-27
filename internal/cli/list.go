package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
	"github.com/hailerity/devrun/internal/ipc"
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

	socketPath := config.SocketPath()
	// Start daemon if not running so the list always reflects live state.
	// If it fails to start, fall through to the offline fallback below.
	_ = daemon.EnsureDaemon(socketPath)

	c, err := client.Connect(socketPath)
	if err != nil {
		return listOffline(reg)
	}
	defer c.Close()

	resp, err := c.Send("list", struct{}{})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}

	var payload ipc.ListResponsePayload
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return fmt.Errorf("parse list response: %w", err)
	}

	// Scope the daemon's full list to the active config, filling in services that
	// have never been started as "stopped".
	printServiceTable(scopeToRegistry(payload.Services, reg))
	return nil
}

// scopeToRegistry filters daemon-reported services down to the names present in
// reg, appending registry-only services as "stopped", sorted by name.
func scopeToRegistry(svcs []ipc.ServiceInfo, reg *config.Registry) []ipc.ServiceInfo {
	byName := make(map[string]ipc.ServiceInfo, len(svcs))
	for _, s := range svcs {
		byName[s.Name] = s
	}
	names := make([]string, 0, len(reg.Services))
	for name := range reg.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ipc.ServiceInfo, 0, len(names))
	for _, name := range names {
		info, ok := byName[name]
		if !ok {
			info = ipc.ServiceInfo{Name: name, State: string(config.StatusStopped)}
		}
		if cfg := reg.Services[name]; cfg != nil && cfg.Group != "" {
			info.Group = cfg.Group
		}
		out = append(out, info)
	}
	return out
}

// listOffline reads the active registry and last-saved state file directly.
// It is called when the daemon is not running. The table is scoped to the
// services defined in reg.
func listOffline(reg *config.Registry) error {
	fmt.Fprintln(os.Stderr, "(daemon not running — showing last known state)")

	state, err := config.LoadState(config.StatePath())
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	names := make([]string, 0, len(reg.Services))
	for name := range reg.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	var svcs []ipc.ServiceInfo
	for _, name := range names {
		svcState := state.Services[name]
		info := ipc.ServiceInfo{Name: name}

		if reg.Services[name] != nil {
			info.Group = reg.Services[name].Group
		}

		if svcState == nil {
			info.State = string(config.StatusStopped)
		} else {
			status := svcState.Status
			// If the state file says running/starting, verify the process is still alive.
			if (status == config.StatusRunning || status == config.StatusStarting) && svcState.PID != nil {
				if syscall.Kill(*svcState.PID, 0) != nil {
					status = config.StatusCrashed
				} else {
					info.PID = svcState.PID
					info.Port = svcState.Port
				}
			}
			info.State = string(status)
		}
		svcs = append(svcs, info)
	}

	printServiceTable(svcs)
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
