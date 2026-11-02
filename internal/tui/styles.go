package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"github.com/charmbracelet/x/ansi"
)

// navKeys moves through tables and text. Single letters other than vim
// motions are left free for actions.
var navKeys = table.KeyMap{
	LineUp:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	LineDown:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	PageUp:       key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup/^b", "page up")),
	PageDown:     key.NewBinding(key.WithKeys("pgdown", "ctrl+f", "space"), key.WithHelp("pgdn/^f", "page down")),
	HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("^u", "½ page up")),
	HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("^d", "½ page down")),
	GotoTop:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
	GotoBottom:   key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
}

// Action keys shared by the views.
var (
	keyOpen   = key.NewBinding(key.WithKeys("enter", "l", "right"), key.WithHelp("enter", "open"))
	keySearch = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search"))
	keyYank   = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy id"))
	keyWeb    = key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "web"))
	keyReload = key.NewBinding(key.WithKeys("r", "ctrl+r"), key.WithHelp("r", "reload"))
	keyAll    = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "search all"))
	keyJSON   = key.NewBinding(key.WithKeys("J"), key.WithHelp("J", "raw JSON"))
	keySchema = key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "schema"))

	keyClearSchema = key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "clear filter"))
)

func truncate(s string, width int) string {
	return ansi.Truncate(s, max(width, 0), "…")
}
