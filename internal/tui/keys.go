package tui

import "github.com/charmbracelet/bubbles/key"

type keymap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Home      key.Binding
	End       key.Binding
	Toggle    key.Binding
	MarkAll   key.Binding
	UnmarkAll key.Binding
	MarkSafe  key.Binding
	Search    key.Binding
	NextCat   key.Binding
	PrevCat   key.Binding
	Sort      key.Binding
	Apply     key.Binding
	Delete    key.Binding
	Save      key.Binding
	Edit      key.Binding
	Rescan    key.Binding
	Options   key.Binding
	Help      key.Binding
	Quit      key.Binding
	Confirm   key.Binding
	Cancel    key.Binding
	Begin     key.Binding
}

func newKeymap() keymap {
	return keymap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:    key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("ctrl+u", "page up")),
		PageDown:  key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("ctrl+d", "page down")),
		Home:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
		End:       key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
		Toggle:    key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "mark")),
		MarkAll:   key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "mark all visible")),
		UnmarkAll: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "unmark all")),
		MarkSafe:  key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "mark all regenerable")),
		Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		NextCat:   key.NewBinding(key.WithKeys("right", "l", "]", "tab"), key.WithHelp("→", "next tab")),
		PrevCat:   key.NewBinding(key.WithKeys("left", "h", "["), key.WithHelp("←", "prev tab")),
		Sort:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Apply:     key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "apply (trash marked)")),
		Delete:    key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "delete marked, no trash")),
		Save:      key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "save report")),
		Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit report in $EDITOR")),
		Rescan:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "re-scan")),
		Options:   key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "scan options")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Confirm:   key.NewBinding(key.WithKeys("y", "Y", "enter"), key.WithHelp("y/enter", "confirm")),
		Cancel:    key.NewBinding(key.WithKeys("n", "N", "esc"), key.WithHelp("n/esc", "cancel")),
		Begin:     key.NewBinding(key.WithKeys(" ", "enter"), key.WithHelp("space/enter", "begin")),
	}
}
