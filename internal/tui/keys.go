package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Tab    key.Binding
	Start  key.Binding
	Stop   key.Binding
	Quit   key.Binding
	Follow key.Binding
	Copy   key.Binding
	Visual key.Binding
	Escape key.Binding
	Top    key.Binding
	Bottom key.Binding
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
	Down:   key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
	Left:   key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "sidebar")),
	Right:  key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "main")),
	Tab:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "switch tab")),
	Start:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start")),
	Stop:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Follow: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "follow")),
	Copy:   key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy")),
	Visual: key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "select")),
	Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "cancel")),
	Top:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top")),
	Bottom: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
}
