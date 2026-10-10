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
	fieldGroup
	editFieldCount
)

var editFieldLabels = [editFieldCount]string{"name", "command", "cwd", "group"}

// editPanel is the modal service editor: four text fields (name / command /
// cwd / group) over the selected service, with Tab/Shift-Tab moving focus and a
// validate() gate before save. It is not part of the sidebar/main focus model —
// while open it consumes all key input.
type editPanel struct {
	open     bool
	origName string // the service being edited; a changed name field means a rename
	// What the daemon runs, as the form opened: a save compares against these to
	// tell whether the edit needs the process restarted.
	origCommand string
	origCWD     string
	inputs      [editFieldCount]textinput.Model
	focus       editField
	errMsg      string
}

// originals returns the name, command and cwd the form opened with — what the
// daemon is running right now. The caller compares against them to decide
// whether an edit needs the process restarted; it does the comparing because
// only it knows the config scope, and a project service's cwd has to be
// compared in its stored form rather than as the form shows it.
func (p *editPanel) originals() (name, command, cwd string) {
	return p.origName, p.origCommand, p.origCWD
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

// openFor prefills the form for service `name` with cfg and focuses the name
// field.
//
// The group needs two values that cfg cannot supply. `own` is the group the
// service sets for itself in the file, which is what the field holds and what
// a save writes back — prefilling cfg.Group instead would stamp the project's
// name into the committed devrun.yaml the first time any field was edited,
// since ToServiceConfigs has already applied it as the default. `inherited` is
// what an empty field falls back to, shown as the placeholder.
//
// Both come from the caller rather than being compared here: `own == inherited`
// is a real state (a service deliberately pinned to the project's name) and
// deriving one from the other cannot tell it from inheritance.
func (p *editPanel) openFor(name string, cfg *config.ServiceConfig, own, inherited string) {
	p.open = true
	p.origName = name
	p.errMsg = ""
	p.focus = fieldName

	vals := [editFieldCount]string{fieldName: name, fieldGroup: own}
	if cfg != nil {
		vals[fieldCommand] = cfg.Command
		vals[fieldCWD] = cfg.CWD
	}
	p.inputs[fieldGroup].Placeholder = inherited
	// The group's own cap, shared with the MCP server's refusal, rather than the
	// 512 the other fields take: a command or a path is legitimately long, a
	// label drawn on one row of a 47-column pane is not.
	p.inputs[fieldGroup].CharLimit = config.MaxGroupLen
	// What the daemon actually runs, kept so a save can tell a relabelling from
	// a change that needs the process restarted.
	p.origCommand = vals[fieldCommand]
	p.origCWD = strings.TrimSpace(vals[fieldCWD])
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

// values returns the trimmed name/cwd/group and the raw command.
func (p *editPanel) values() (name, command, cwd, group string) {
	return strings.TrimSpace(p.inputs[fieldName].Value()),
		p.inputs[fieldCommand].Value(),
		strings.TrimSpace(p.inputs[fieldCWD].Value()),
		strings.TrimSpace(p.inputs[fieldGroup].Value())
}

// validate returns the first blocking problem, or "" when the form can be saved.
// existing is the set of all current service names.
//
// The group is deliberately unvalidated beyond trimming: it is a display label
// the sidebar sections by, never part of a service's identity and never a path
// or a filename, so there is nothing for a name rule to protect. Clearing it is
// meaningful too — a project service falls back to the project's name.
func (p *editPanel) validate(existing map[string]bool) string {
	name, command, _, _ := p.values()
	switch {
	case name == "":
		return "name cannot be empty"
	case name != p.origName && config.ValidateName("service", name) != nil:
		// A rename must follow the name rule; keeping an existing name need not.
		return config.ValidateName("service", name).Error()
	case strings.TrimSpace(command) == "":
		return "command cannot be empty"
	case name != p.origName && existing[name]:
		return fmt.Sprintf("a service named %q already exists", name)
	}
	return ""
}

func (p editPanel) view() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", styleAccent.Bold(true).Render("Edit "+p.origName))
	// Above the fields, not below them. overlay() clips the modal from the
	// bottom, and at four fields the panel is tall enough that a short terminal
	// loses its last rows — which would make a refused save look like a key
	// that did nothing. The reason it was refused is the one line that cannot
	// afford to be the casualty.
	if p.errMsg != "" {
		fmt.Fprintf(&b, "%s\n\n", styleRed.Render(p.errMsg))
	}
	for i := range p.inputs {
		label := editFieldLabels[i]
		if editField(i) == p.focus {
			label = styleAccent.Render("▸ " + label)
		} else {
			label = styleMuted.Render("  " + label)
		}
		fmt.Fprintf(&b, "%s\n  %s\n", label, p.inputs[i].View())
	}
	b.WriteString("\n" + styleMuted.Render("Tab move · Enter save · Esc cancel"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Render(b.String())
}
