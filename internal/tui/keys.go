package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Tab      key.Binding
	Enter    key.Binding
	Start    key.Binding
	Stop     key.Binding
	Edit     key.Binding
	Remove   key.Binding
	Quit     key.Binding
	Follow   key.Binding
	Copy     key.Binding
	Visual   key.Binding
	Escape   key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Wrap     key.Binding
	Target   key.Binding
	StartAll key.Binding
	StopAll  key.Binding
	Help     key.Binding
	Search   key.Binding
	Next     key.Binding
	Prev     key.Binding
	Restart  key.Binding
}

// The help text here is what the `?` overlay prints, so each description says
// what the key does in full rather than the footer's one-word label.
var keys = keyMap{
	Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k / ↑", "move up")),
	Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j / ↓", "move down")),
	Left:     key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "focus the service list")),
	Right:    key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "focus the log pane")),
	Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "switch pane")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("↵", "logs ⇄ details")),
	Start:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start the selected service")),
	Stop:     key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop the selected service")),
	Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit the selected service")),
	Remove:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove the selected service")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Follow:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "toggle follow")),
	Copy:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy the line or selection")),
	Visual:   key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "select lines")),
	Escape:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "cancel / back to logs")),
	Top:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "jump to the first line")),
	Bottom:   key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "jump to the end and follow")),
	Wrap:     key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "toggle line wrap")),
	Target:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "filter by target")),
	StartAll: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "start everything listed")),
	StopAll:  key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "stop everything listed")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "show this help")),
	Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search the log")),
	Next:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match (down)")),
	Prev:     key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "previous match (up)")),
	Restart:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart the selected service")),
}
