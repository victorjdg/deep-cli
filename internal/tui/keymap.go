package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Submit      key.Binding
	Quit        key.Binding
	Cancel      key.Binding
	ClearScreen key.Binding
	Newline     key.Binding
	ToggleAuto  key.Binding
}

var keys = keyMap{
	Submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "quit"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "cancel/quit"),
	),
	ClearScreen: key.NewBinding(
		key.WithKeys("ctrl+l"),
		key.WithHelp("ctrl+l", "clear screen"),
	),
	Newline: key.NewBinding(
		key.WithKeys("shift+enter"),
		key.WithHelp("shift+enter", "newline"),
	),
	ToggleAuto: key.NewBinding(
		key.WithKeys("ctrl+a"),
		key.WithHelp("ctrl+a", "toggle auto-accept"),
	),
}
