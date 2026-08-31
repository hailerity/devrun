package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/ipc"
)

// targetDetailsPanel renders a read-only roll-up of the focused target in the
// main pane: the target's name and active state plus every member service with
// its own state, port, and PID. It is the target-row counterpart of
// detailsPanel and, like it, is not a focus target.
type targetDetailsPanel struct{}

// render draws the panel for target t. infos holds the daemon-reported
// ServiceInfo for whichever of t's members the daemon knows about; a declared
// member with no entry is shown as "not reported". Callers pass a nil or
// synthetic "All services" row through as the empty state.
func (tp targetDetailsPanel) render(t *sidebarTarget, infos []ipc.ServiceInfo, width, height int) string {
	if t == nil || t.name == "" {
		return styleMuted.Render("No target selected")
	}

	byName := make(map[string]ipc.ServiceInfo, len(infos))
	for _, s := range infos {
		byName[s.Name] = s
	}

	running := 0
	for _, name := range t.members {
		if s, ok := byName[name]; ok && s.State == "running" {
			running++
		}
	}

	var sb strings.Builder

	// Summary table — sits directly under the bordered "TARGET" view label that
	// renderMain draws, so it needs no header of its own. "state" reports the
	// target's activation (whether `devrun target start` holds it), not a
	// member roll-up — the "services" row carries the live running count, and
	// an active target can legitimately have zero members up.
	state := styleMuted.Render("○ stopped")
	if t.active {
		state = styleGreen.Render("● started")
	}
	sb.WriteString(renderTable([][]string{
		{"name", styleText.Render(t.name)},
		{"state", state},
		{"services", fmt.Sprintf("%d running / %d", running, len(t.members))},
	}))

	// SERVICES — declared order, so the list matches the target definition.
	sb.WriteString("\n" + styleMuted.Render("SERVICES") + "\n")
	if len(t.members) == 0 {
		sb.WriteString("  " + styleMuted.Render("(no services)") + "\n")
	}
	for _, name := range t.members {
		s, ok := byName[name]
		if !ok {
			fmt.Fprintf(&sb, "  %s %s  %s\n",
				styleMuted.Render("○"),
				styleText.Render(name),
				styleMuted.Render("not reported"),
			)
			continue
		}
		fmt.Fprintf(&sb, "  %s %s  %s  %s\n",
			stateDot(s.State),
			styleText.Render(name),
			renderPort(s.Port),
			styleMuted.Render("pid ")+renderPID(s.PID),
		)
	}

	return lipgloss.NewStyle().Width(width).Height(height).Render(sb.String())
}
