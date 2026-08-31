package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hailerity/devrun/internal/config"
)

type editField int

const (
	fieldName editField = iota
	fieldCommand
	fieldCWD
	editFieldCount
)

var editFieldLabels = [editFieldCount]string{"name", "command", "cwd"}

// editPanel is the modal service editor: three text fields (name / command /
// cwd) over the selected service, with Tab/Shift-Tab moving focus and a
// validate() gate before save. It is not part of the sidebar/main focus model —
// while open it consumes all key input.
type editPanel struct {
	open     bool
	origName string // the service being edited; a changed name field means a rename
	inputs   [editFieldCount]textinput.Model
	focus    editField
	errMsg   string
}

func newEditPanel() editPanel {
	var p editPanel
	for i := range p.inputs {
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 512
		ti.Width = 44
		p.inputs[i] = ti
	}
	return p
}

// openFor prefills the form for service `name` with cfg and focuses the name field.
func (p *editPanel) openFor(name string, cfg *config.ServiceConfig) {
	p.open = true
	p.origName = name
	p.errMsg = ""
	p.focus = fieldName

	vals := [editFieldCount]string{fieldName: name}
	if cfg != nil {
		vals[fieldCommand] = cfg.Command
		vals[fieldCWD] = cfg.CWD
	}
	for i := range p.inputs {
		p.inputs[i].SetValue(vals[i])
		p.inputs[i].CursorEnd()
		p.inputs[i].Blur()
	}
	p.inputs[p.focus].Focus()
}

func (p *editPanel) close() {
	p.open = false
	for i := range p.inputs {
		p.inputs[i].Blur()
	}
}

// focusDelta moves the focused field by d (wrapping), e.g. +1 for Tab, -1 for Shift-Tab.
func (p *editPanel) focusDelta(d int) {
	p.inputs[p.focus].Blur()
	n := int(editFieldCount)
	p.focus = editField((int(p.focus) + d%n + n) % n)
	p.inputs[p.focus].Focus()
}

// update routes a message to the focused input; the caller handles Tab/Enter/Esc.
func (p *editPanel) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	p.inputs[p.focus], cmd = p.inputs[p.focus].Update(msg)
	return cmd
}

// values returns the trimmed name/cwd and the raw command.
func (p *editPanel) values() (name, command, cwd string) {
	return strings.TrimSpace(p.inputs[fieldName].Value()),
		p.inputs[fieldCommand].Value(),
		strings.TrimSpace(p.inputs[fieldCWD].Value())
}

// validate returns the first blocking problem, or "" when the form can be saved.
// existing is the set of all current service names.
func (p *editPanel) validate(existing map[string]bool) string {
	name, command, _ := p.values()
	switch {
	case name == "":
		return "name cannot be empty"
	case strings.TrimSpace(command) == "":
		return "command cannot be empty"
	case name != p.origName && existing[name]:
		return fmt.Sprintf("a service named %q already exists", name)
	}
	return ""
}

func (p editPanel) view(width, height int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", styleAccent.Bold(true).Render("Edit "+p.origName))
	for i := range p.inputs {
		label := editFieldLabels[i]
		if editField(i) == p.focus {
			label = styleAccent.Render("▸ " + label)
		} else {
			label = styleMuted.Render("  " + label)
		}
		fmt.Fprintf(&b, "%s\n  %s\n", label, p.inputs[i].View())
	}
	if p.errMsg != "" {
		fmt.Fprintf(&b, "\n%s\n", styleRed.Render(p.errMsg))
	}
	b.WriteString("\n" + styleMuted.Render("Tab move · Enter save · Esc cancel"))

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
