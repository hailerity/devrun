package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all services with status",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

func runList(_ *cobra.Command, _ []string) error {
	socketPath := config.SocketPath()
	c, err := client.Connect(socketPath)
	if err != nil {
		return listOffline()
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

	printServiceTable(payload.Services)
	return nil
}

// listOffline reads the registry and last-saved state file directly.
// It is called when the daemon is not running.
func listOffline() error {
	fmt.Fprintln(os.Stderr, "(daemon not running — showing last known state)")

	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	state, err := config.LoadState(config.StatePath())
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	// Collect names from both registry and state so nothing is hidden.
	seen := make(map[string]bool)
	var names []string
	for name := range reg.Services {
		seen[name] = true
		names = append(names, name)
	}
	for name := range state.Services {
		if !seen[name] {
			names = append(names, name)
		}
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

func printServiceTable(svcs []ipc.ServiceInfo) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tGROUP\tSTATE\tPID\tPORT\tUPTIME\tCPU%\tMEM")
	for _, svc := range svcs {
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
		cpu := "-"
		mem := "-"
		if svc.CPUPct > 0 || svc.MemBytes > 0 {
			cpu = fmt.Sprintf("%.1f%%", svc.CPUPct)
			mem = formatBytes(svc.MemBytes)
		}
		group := svc.Group
		if group == "" {
			group = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			svc.Name, group, svc.State, pid, port, uptime, cpu, mem)
	}
	w.Flush()
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
