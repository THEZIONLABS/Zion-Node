package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds all TUI key bindings.
type keyMap struct {
	Tab1   key.Binding
	Tab2   key.Binding
	Tab3   key.Binding
	Tab4   key.Binding
	Quit   key.Binding
	Up     key.Binding
	Down   key.Binding
	PgUp   key.Binding
	PgDown key.Binding
	Home   key.Binding
	End    key.Binding
	Filter key.Binding
	Enter  key.Binding
}

var keys = keyMap{
	Tab1:   key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "Dashboard")),
	Tab2:   key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "Logs")),
	Tab3:   key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "Wallet")),
	Tab4:   key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "Rewards")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "Quit")),
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑", "Up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓", "Down")),
	PgUp:   key.NewBinding(key.WithKeys("pgup")),
	PgDown: key.NewBinding(key.WithKeys("pgdown")),
	Home:   key.NewBinding(key.WithKeys("home")),
	End:    key.NewBinding(key.WithKeys("end")),
	Filter: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "Filter")),
	Enter:  key.NewBinding(key.WithKeys("enter")),
}
