package cli

import (
	"encoding/json"
	"fmt"
	"os"
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
		return fmt.Errorf("connect to daemon: %w", err)
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tGROUP\tSTATE\tPID\tPORT\tUPTIME\tCPU%\tMEM")
	for _, svc := range payload.Services {
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
	return nil
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
