package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// targetEditRows caps how many service rows the checklist shows at once; the
// window scrolls to keep the cursor visible.
const targetEditRows = 10

// targetEditPanel is the modal target editor: a name field plus a checklist of
// every service, with membership toggled by space. Tab switches focus between
// the name field and the list. Like editPanel it is a keyboard trap while open.
type targetEditPanel struct {
	open      bool
	origName  string
	nameInput textinput.Model
	services  []string        // every registry service name, sorted
	member    map[string]bool // membership state keyed by service name
	cursor    int             // index into services
	top       int             // first visible services index
	focusName bool            // true: name field focused; false: the list
	errMsg    string
}

func newTargetEditPanel() targetEditPanel {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 128
	ti.Width = 40
	return targetEditPanel{nameInput: ti}
}

// openFor prefills the modal for target `name`: allServices is the full sorted
// service list, members the names currently in the target.
func (p *targetEditPanel) openFor(name string, allServices, members []string) {
	p.open = true
	p.origName = name
	p.errMsg = ""
	p.cursor = 0
	p.top = 0
	p.focusName = true

	p.services = append([]string(nil), allServices...)
	sort.Strings(p.services)

	p.member = make(map[string]bool, len(members))
	for _, m := range members {
		p.member[m] = true
	}

	p.nameInput.SetValue(name)
	p.nameInput.CursorEnd()
	p.nameInput.Focus()
}

func (p *targetEditPanel) close() {
	p.open = false
	p.nameInput.Blur()
}

// focusSwap moves focus between the name field and the service list.
func (p *targetEditPanel) focusSwap() {
	p.focusName = !p.focusName
	if p.focusName {
		p.nameInput.Focus()
	} else {
		p.nameInput.Blur()
	}
}

// moveCursor moves the list cursor by d, clamped, and scrolls the window to keep
// it visible.
func (p *targetEditPanel) moveCursor(d int) {
	if len(p.services) == 0 {
		return
	}
	p.cursor += d
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.services) {
		p.cursor = len(p.services) - 1
	}
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+targetEditRows {
		p.top = p.cursor - targetEditRows + 1
	}
}

// toggleAtCursor flips membership of the service under the list cursor.
func (p *targetEditPanel) toggleAtCursor() {
	if len(p.services) == 0 {
		return
	}
	name := p.services[p.cursor]
	if p.member[name] {
		delete(p.member, name)
	} else {
		p.member[name] = true
	}
}

// update routes a message to the name field (only meaningful while it is focused).
func (p *targetEditPanel) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	p.nameInput, cmd = p.nameInput.Update(msg)
	return cmd
}

// values returns the trimmed name and the sorted member list.
func (p *targetEditPanel) values() (name string, members []string) {
	name = strings.TrimSpace(p.nameInput.Value())
	for _, s := range p.services {
		if p.member[s] {
			members = append(members, s)
		}
	}
	return name, members
}

// validate returns the first blocking problem, or "" when the modal can be saved.
// existing is the set of all current target names.
func (p *targetEditPanel) validate(existing map[string]bool) string {
	name, _ := p.values()
	switch {
	case name == "":
		return "name cannot be empty"
	case name != p.origName && existing[name]:
		return fmt.Sprintf("a target named %q already exists", name)
	}
	return ""
}

func (p targetEditPanel) view(width, height int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", styleAccent.Bold(true).Render("Edit target "+p.origName))

	nameLabel := "  name"
	if p.focusName {
		nameLabel = styleAccent.Render("▸ name")
	} else {
		nameLabel = styleMuted.Render("  name")
	}
	fmt.Fprintf(&b, "%s\n  %s\n\n", nameLabel, p.nameInput.View())

	svcLabel := "  services"
	if !p.focusName {
		svcLabel = styleAccent.Render("▸ services")
	} else {
		svcLabel = styleMuted.Render("  services")
	}
	b.WriteString(svcLabel + "\n")

	if len(p.services) == 0 {
		b.WriteString("  " + styleMuted.Render("(no services)") + "\n")
	}
	end := p.top + targetEditRows
	if end > len(p.services) {
		end = len(p.services)
	}
	for i := p.top; i < end; i++ {
		name := p.services[i]
		box := "[ ]"
		if p.member[name] {
			box = styleGreen.Render("[x]")
		}
		row := fmt.Sprintf("  %s %s", box, name)
		if !p.focusName && i == p.cursor {
			row = styleAccent.Render("▸ ") + box + " " + styleText.Render(name)
		}
		b.WriteString(row + "\n")
	}
	if len(p.services) > targetEditRows {
		fmt.Fprintf(&b, "  %s\n", styleMuted.Render(fmt.Sprintf("%d–%d of %d", p.top+1, end, len(p.services))))
	}

	if p.errMsg != "" {
		fmt.Fprintf(&b, "\n%s\n", styleRed.Render(p.errMsg))
	}
	b.WriteString("\n" + styleMuted.Render("Tab move · Space toggle · Enter save · Esc cancel"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Render(b.String())
	if height < lipgloss.Height(box) {
		height = lipgloss.Height(box)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
