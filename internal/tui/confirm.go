package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// removeConfirm is the modal shown before a service is removed from the active
// config. Like the edit modals it is a keyboard trap while open, but it carries
// no fields — only y (confirm) and n / Esc (cancel).
type removeConfirm struct {
	open    bool
	name    string // the service to be removed
	errMsg  string // a daemon or persistence failure; keeps the modal open
	pending bool   // a confirm is in flight — the daemon has been asked, no reply yet
}

// openFor arms the confirm for service name.
func (c *removeConfirm) openFor(name string) {
	c.open = true
	c.name = name
	c.errMsg = ""
	c.pending = false
}

func (c *removeConfirm) close() {
	c.open = false
	c.errMsg = ""
	c.pending = false
}

func (c removeConfirm) view(width, height int) string {
	var b strings.Builder
	b.WriteString(styleRed.Bold(true).Render("Remove "+c.name) + "\n\n")
	b.WriteString(styleText.Render("Remove this service from the active config?") + "\n")
	b.WriteString(styleMuted.Render("Its definition is removed from the file — this cannot be undone.") + "\n")
	if c.errMsg != "" {
		b.WriteString("\n" + styleRed.Render(c.errMsg) + "\n")
	}
	b.WriteString("\n" + styleMuted.Render("y remove · n/Esc cancel"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorRed).
		Padding(1, 2).
		Render(b.String())
	if height < lipgloss.Height(box) {
		height = lipgloss.Height(box)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
