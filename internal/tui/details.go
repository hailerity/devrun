package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

// detailsPanel is the DETAILS view of the selected service: status, config and
// environment as label/value rows. It takes focus like the log pane — a cursor
// walks the value rows, the list scrolls to keep it visible, and y copies the
// value under it.
type detailsPanel struct {
	cursor int // index among the value rows (section headings are skipped)
	top    int // first visible line of the rendered list
	rows   int // visible line count, set by the model's layout; 0 = not laid out
}

// detailLine is one rendered line: a section heading or spacer (value < 0), or
// a label/value row that the cursor can land on.
type detailLine struct {
	text  string // styled, without the cursor highlight
	value int    // index among the value rows, or -1
	label string
	copy  string // what y puts on the clipboard: the raw value, unstyled
}

// detailLines builds the view's lines from the live service state and its
// config. It is rebuilt on every use rather than cached: the values (uptime,
// CPU, state) change with each daemon poll.
func detailLines(svc *ipc.ServiceInfo, cfg *config.ServiceConfig) []detailLine {
	if svc == nil {
		return nil
	}
	var out []detailLine
	n := 0
	section := func(title string, rows [][3]string) {
		if len(out) > 0 {
			out = append(out, detailLine{value: -1})
		}
		out = append(out, detailLine{text: styleMuted.Render(title), value: -1})
		labelW := 0
		for _, r := range rows {
			labelW = max(labelW, lipgloss.Width(r[0]))
		}
		for _, r := range rows {
			out = append(out, detailLine{
				text:  "  " + styleMuted.Render(padRight(r[0], labelW+2)) + r[1],
				value: n,
				label: r[0],
				copy:  r[2],
			})
			n++
		}
	}

	pid, port := "", ""
	if svc.PID != nil {
		pid = fmt.Sprintf("%d", *svc.PID)
	}
	if svc.Port != nil && *svc.Port != 0 {
		port = fmt.Sprintf("%d", *svc.Port)
	}
	section("STATUS", [][3]string{
		{"state", renderStateLabel(svc.State), svc.State},
		{"pid", renderPID(svc.PID), pid},
		{"port", renderPort(svc.Port), port},
		{"uptime", formatUptimeFull(svc.UptimeSec), formatUptimeFull(svc.UptimeSec)},
		{"cpu", renderCPUFull(svc.CPUPct), fmt.Sprintf("%.1f%%", svc.CPUPct)},
		{"mem", formatBytes(svc.MemBytes), formatBytes(svc.MemBytes)},
		{"started", computeStarted(svc.UptimeSec), ansi.Strip(computeStarted(svc.UptimeSec))},
	})
	if cfg == nil {
		return out
	}

	cfgRows := [][3]string{
		{"cmd", styleText.Render(cfg.Command), cfg.Command},
		{"cwd", styleMuted.Render(cfg.CWD), cfg.CWD},
	}
	if cfg.Group != "" {
		cfgRows = append(cfgRows, [3]string{"group", styleMuted.Render(cfg.Group), cfg.Group})
	}
	section("CONFIG", cfgRows)

	if len(cfg.Env) > 0 {
		var env [][3]string
		for _, k := range sortedStringKeys(cfg.Env) {
			env = append(env, [3]string{k, styleAccent.Render(cfg.Env[k]), cfg.Env[k]})
		}
		section("ENV", env)
	}
	return out
}

func valueCount(lines []detailLine) int {
	n := 0
	for _, l := range lines {
		if l.value >= 0 {
			n++
		}
	}
	return n
}

// setRows tells the panel how many lines its pane can show.
func (dp *detailsPanel) setRows(n int) { dp.rows = n }

// reset returns to the top — for when another service is selected.
func (dp *detailsPanel) reset() { dp.cursor, dp.top = 0, 0 }

// move shifts the cursor by d over the value rows of lines (clamped, no wrap —
// like the log pane) and scrolls it into view.
func (dp *detailsPanel) move(d int, lines []detailLine) {
	dp.cursor = max(0, min(dp.cursor+d, valueCount(lines)-1))
	dp.scrollToCursor(lines)
}

// scrollToCursor moves the window the least it must to show the cursor's line.
// On the first value row it snaps to the very top instead, so the section
// heading above it is not left just out of view.
func (dp *detailsPanel) scrollToCursor(lines []detailLine) {
	dp.cursor = max(0, min(dp.cursor, valueCount(lines)-1))
	if dp.rows <= 0 {
		dp.top = 0
		return
	}
	at := 0
	for i, l := range lines {
		if l.value == dp.cursor {
			at = i
		}
	}
	switch {
	case dp.cursor == 0:
		dp.top = 0
	case at < dp.top:
		dp.top = at
	case at >= dp.top+dp.rows:
		dp.top = at - dp.rows + 1
	}
	dp.top = max(0, min(dp.top, len(lines)-dp.rows))
}

// selected returns the value row under the cursor, or nil.
func (dp *detailsPanel) selected(lines []detailLine) *detailLine {
	for i := range lines {
		if lines[i].value >= 0 && lines[i].value == dp.cursor {
			return &lines[i]
		}
	}
	return nil
}

// window returns the half-open range of lines currently visible.
func (dp detailsPanel) window(lines []detailLine) (first, last int) {
	if dp.rows <= 0 {
		return 0, len(lines)
	}
	first = max(0, min(dp.top, len(lines)))
	return first, min(len(lines), first+dp.rows)
}

// render draws the visible window `width` columns wide. The cursor row is
// highlighted only while the pane has focus — unfocused, DETAILS is a plain
// read-out that updates as the sidebar cursor walks the services.
func (dp detailsPanel) render(lines []detailLine, width int, focused bool) string {
	if len(lines) == 0 {
		return styleMuted.Render("No service selected")
	}
	first, last := dp.window(lines)
	out := make([]string, 0, last-first)
	for _, l := range lines[first:last] {
		if focused && l.value >= 0 && l.value == dp.cursor {
			// Same treatment as the log cursor: an accent gutter bar plus a
			// full-width background. Drawn from the unstyled text, because an
			// SGR reset inside a styled value would punch a hole in the bar.
			out = append(out, styleSelectedLine.Width(max(0, width-1)).
				Render(ansi.Truncate(ansi.Strip(l.text), max(0, width-1), "…")))
			continue
		}
		out = append(out, ansi.Truncate(" "+l.text, width, "…"))
	}
	return strings.Join(out, "\n")
}

func renderStateLabel(state string) string {
	glyph, fg := stateGlyph(state)
	return lipgloss.NewStyle().Foreground(fg).Render(glyph + " " + state)
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

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
