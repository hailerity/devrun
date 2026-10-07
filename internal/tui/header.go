package tui

import (
	"fmt"
	"strings"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type headerBar struct{}

// render draws the one-row header: the app name and the config in scope on the
// left, the project-wide running count — plus a red crashed count when anything
// is down — and the poll indicator on the right.
func (h headerBar) render(source string, total, running, crashed, frame int, spinning bool, gw *ipc.GatewayStatusPayload, width int) string {
	left := " " + styleAccent.Bold(true).Render("⬡ devrun")
	if source != "" {
		left += "  " + styleMuted.Render(source)
	}

	if label := gatewayLabel(gw); label != "" {
		left += "  " + label
	}

	count := styleMuted
	if running > 0 {
		count = styleGreen
	}
	right := count.Render(fmt.Sprintf("%d/%d running", running, total))
	if crashed > 0 {
		right += "  " + styleRed.Render(fmt.Sprintf("✖ %d crashed", crashed))
	}
	indicator := " "
	if spinning {
		indicator = spinFrames[frame%len(spinFrames)]
	}
	// The poll indicator owns the last column, above the main pane's corner, so
	// the counts line up with the pane edge whether or not it is spinning.
	right += " " + styleMuted.Render(indicator)

	// The counts matter more than the config label: drop the label before
	// letting the line outgrow the terminal.
	if lipgloss.Width(left)+1+lipgloss.Width(right) > width {
		left = " " + styleAccent.Bold(true).Render("⬡ devrun")
	}
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	// lipgloss's Width() only pads a short line, it never caps a long one, and
	// an overlong header wraps on the real terminal and scrolls the layout —
	// so truncate as the last word on width.
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, width, "")
}

// gatewayLabel is the header's word on the gateway, and empty when none runs.
//
// Published is amber, not green: it is the state worth noticing, because
// something off this machine can reach the services. A tunnel says so in the
// word, since "published" alone does not distinguish a tunnel devrun runs
// from a proxy someone else put in front.
//
// A running tunnel counts as published whatever the config says, because it
// is: traffic through it carries the public hostname, which the gateway's own
// Posture judges as off this machine. Reading only the configured posture
// showed a quiet "gateway" at the exact moment the exposed services were
// reachable from the internet.
func gatewayLabel(gw *ipc.GatewayStatusPayload) string {
	if gw == nil || !gw.Running {
		return ""
	}
	word := ""
	switch {
	case gw.Tunnel != nil && gw.Tunnel.Running:
		word = "tunnelled"
	case gw.Posture == config.PosturePublished:
		word = "published"
	default:
		// Green rather than muted: the gateway running is a thing that is on,
		// and grey reads as off.
		return styleGreen.Render("◎ gateway")
	}
	// The count is the question anyone reading this actually has — published
	// says the door is open, and this says how much is behind it. Zero is
	// worth printing loudest of all: a tunnel with nothing exposed serves 404
	// to every service, which looks like a broken tunnel.
	return styleYellow.Render(fmt.Sprintf("◉ %s %d", word, len(gw.Exposed)))
}
