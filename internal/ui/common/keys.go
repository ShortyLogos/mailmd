package common

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Up, Down, Open, Back, Compose, Reply, Forward, Trash    key.Binding
	Preview, BPreview, NextTab, PrevTab, Send, Edit          key.Binding
	Refresh, Restore, Select, SelectAll, Quit, Help          key.Binding
	Home, End                                                key.Binding
}

var Keys = KeyMap{
	Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "up")),
	Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down")),
	Open:     key.NewBinding(key.WithKeys("enter", "o", "right", "l"), key.WithHelp("o", "open")),
	Back:     key.NewBinding(key.WithKeys("esc", "backspace", "left", "h"), key.WithHelp("esc", "back")),
	Compose:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "compose")),
	Reply:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
	Forward:  key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "forward")),
	Trash:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "trash")),
	Preview:  key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "preview")),
	BPreview: key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "browser preview")),
	NextTab:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next folder")),
	PrevTab:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev folder")),
	Send:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "send")),
	Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Refresh:  key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),
	Restore:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "restore")),
	Select:    key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
	SelectAll: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all")),
	Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Home:     key.NewBinding(key.WithKeys("home", "pgup"), key.WithHelp("Home", "top")),
	End:      key.NewBinding(key.WithKeys("end", "pgdown"), key.WithHelp("End", "bottom")),
}
