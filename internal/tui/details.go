package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

type detailsPanel struct{}

func (dp detailsPanel) render(svc *ipc.ServiceInfo, cfg *config.ServiceConfig, width, height int) string {
	if svc == nil {
		return styleMuted.Render("No service selected")
	}

	var sb strings.Builder

	// STATUS
	sb.WriteString(styleMuted.Render("STATUS") + "\n")
	statusRows := [][]string{
		{"state", renderStateLabel(svc.State)},
		{"pid", renderPID(svc.PID)},
		{"port", renderPort(svc.Port)},
		{"uptime", formatUptimeFull(svc.UptimeSec)},
		{"cpu", renderCPUFull(svc.CPUPct)},
		{"mem", formatBytes(svc.MemBytes)},
		{"started", computeStarted(svc.UptimeSec)},
	}
	sb.WriteString(renderTable(statusRows))

	if cfg == nil {
		return sb.String()
	}

	// CONFIG
	sb.WriteString("\n" + styleMuted.Render("CONFIG") + "\n")
	cfgRows := [][]string{
		{"cmd", styleText.Render(cfg.Command)},
		{"cwd", styleMuted.Render(cfg.CWD)},
	}
	if cfg.Group != "" {
		cfgRows = append(cfgRows, []string{"group", styleMuted.Render(cfg.Group)})
	}
	sb.WriteString(renderTable(cfgRows))

	// ENV section: KEY (muted) = value (accent), matching the design mockup.
	// ENV (omit section if empty)
	if len(cfg.Env) > 0 {
		sb.WriteString("\n" + styleMuted.Render("ENV") + "\n")
		keys := sortedStringKeys(cfg.Env)
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("  %s=%s\n",
				styleMuted.Render(k),
				styleAccent.Render(cfg.Env[k]),
			))
		}
	}

	return lipgloss.NewStyle().Width(width).Height(height).Render(sb.String())
}

func renderStateLabel(state string) string {
	switch state {
	case "running":
		return styleGreen.Render("● running")
	case "crashed":
		return styleRed.Render("● crashed")
	default:
		return styleMuted.Render("● " + state)
	}
}

func renderPID(pid *int) string {
	if pid == nil {
		return styleMuted.Render("—")
	}
	return fmt.Sprintf("%d", *pid)
}

func renderPort(port *int) string {
	if port == nil || *port == 0 {
		return styleMuted.Render("—")
	}
	return styleAccent.Render(fmt.Sprintf(":%d", *port))
}

func renderCPUFull(pct float64) string {
	s := fmt.Sprintf("%.1f%%", pct)
	if pct > 80 {
		return styleRed.Render(s)
	}
	if pct > 50 {
		return styleYellow.Render(s)
	}
	return s
}

// computeStarted approximates the service start time as now - uptime.
// This is intentionally approximate (±2s) since ServiceInfo carries no StartedAt field.
func computeStarted(uptimeSec int64) string {
	if uptimeSec <= 0 {
		return styleMuted.Render("—")
	}
	t := time.Now().Add(-time.Duration(uptimeSec) * time.Second)
	return styleMuted.Render(t.Format("15:04:05"))
}

// formatUptimeFull returns uptime with hours, minutes, and seconds — used in the Details panel.
func formatUptimeFull(sec int64) string {
	if sec <= 0 {
		return "—"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func renderTable(rows [][]string) string {
	maxW := 0
	for _, r := range rows {
		if len(r[0]) > maxW {
			maxW = len(r[0])
		}
	}
	var sb strings.Builder
	for _, r := range rows {
		label := styleMuted.Render(fmt.Sprintf("%-*s", maxW+2, r[0]))
		sb.WriteString("  " + label + r[1] + "\n")
	}
	return sb.String()
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
