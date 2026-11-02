package tui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type dialogDoneMsg struct {
	to  int
	err error
}

func (m dialogDoneMsg) target() int { return m.to }

var (
	keyConfirm = key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "confirm"))
	keyOption  = key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle option"))
)

// confirmView asks before an action. With expect set, the user has to type
// it (like the CLI does for deletions); otherwise y or enter confirms. An
// optional option is toggled with space and passed to run.
type confirmView struct {
	env     *env
	vid     int
	name    string
	message []string
	expect  string
	input   textinput.Model
	option  string
	on      bool
	busy    bool
	err     string
	width   int
	height  int
	run     func(option bool) tea.Cmd
	done    func() tea.Cmd
}

func newConfirmView(e *env, name string, message ...string) *confirmView {
	return &confirmView{env: e, vid: e.nextID(), name: name, message: message}
}

// typed makes the user type expect to confirm.
func (c *confirmView) typed(expect string) *confirmView {
	c.expect = expect
	c.input = textinput.New()
	c.input.Prompt = "❯ "
	return c
}

func (c *confirmView) withOption(label string, on bool) *confirmView {
	c.option, c.on = label, on
	return c
}

func (c *confirmView) id() int         { return c.vid }
func (c *confirmView) title() string   { return c.name }
func (c *confirmView) capturing() bool { return true }

func (c *confirmView) init() tea.Cmd {
	if c.expect != "" {
		return c.input.Focus()
	}
	return nil
}

func (c *confirmView) keys() []key.Binding {
	if c.expect != "" {
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")), keyCancel}
	}
	ks := []key.Binding{keyConfirm}
	if c.option != "" {
		ks = append(ks, keyOption)
	}
	return append(ks, key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "cancel")))
}

func (c *confirmView) resize(width, height int) {
	c.width, c.height = width, height
	c.input.SetWidth(max(min(width-12, 40), 10))
}

func (c *confirmView) confirm() tea.Cmd {
	if c.busy {
		return nil
	}
	if c.expect != "" && c.input.Value() != c.expect {
		c.err = fmt.Sprintf("type %q to confirm", c.expect)
		return nil
	}
	c.busy, c.err = true, ""
	return c.run(c.on)
}

func (c *confirmView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case dialogDoneMsg:
		c.busy = false
		if msg.err != nil {
			c.err = msg.err.Error()
			return nil
		}
		return tea.Batch(pop(c.vid), c.done())
	case tea.KeyPressMsg:
		if msg.String() == "esc" {
			return pop(c.vid)
		}
		if c.expect != "" {
			if msg.String() == "enter" {
				return c.confirm()
			}
			var cmd tea.Cmd
			c.input, cmd = c.input.Update(msg)
			return cmd
		}
		switch {
		case key.Matches(msg, keyConfirm):
			return c.confirm()
		case c.option != "" && key.Matches(msg, keyOption):
			c.on = !c.on
		case msg.String() == "n":
			return pop(c.vid)
		}
		return nil
	}
	if c.expect != "" {
		var cmd tea.Cmd
		c.input, cmd = c.input.Update(msg)
		return cmd
	}
	return nil
}

func (c *confirmView) render() string {
	t := c.env.theme
	rows := []string{t.title.Render(c.name), ""}
	rows = append(rows, c.message...)
	if c.option != "" {
		box := "[ ]"
		if c.on {
			box = "[x]"
		}
		rows = append(rows, "", t.focus.Render(box)+" "+c.option+t.dim.Render("  (space)"))
	}
	if c.expect != "" {
		rows = append(rows, "", fmt.Sprintf("Type %s to confirm:", t.focus.Render(c.expect)), c.input.View())
	}
	rows = append(rows, "")
	switch {
	case c.busy:
		rows = append(rows, t.dim.Render("working…"))
	case c.err != "":
		rows = append(rows, t.err.Render(c.err))
	case c.expect != "":
		rows = append(rows, t.dim.Render("enter confirm · esc cancel"))
	default:
		rows = append(rows, t.dim.Render("y confirm · n cancel"))
	}
	return centered(t, rows, c.width, c.height)
}

// menuItem is one entry of an actions menu.
type menuItem struct {
	key key.Binding
	run func() tea.Cmd
}

// menuView lists actions with their keys. Enter runs the selected one, and
// each action's own key runs it directly.
type menuView struct {
	env    *env
	vid    int
	name   string
	items  []menuItem
	cursor int
	width  int
	height int
}

func newMenuView(e *env, name string, items []menuItem) *menuView {
	return &menuView{env: e, vid: e.nextID(), name: name, items: items}
}

func (m *menuView) id() int                  { return m.vid }
func (m *menuView) title() string            { return m.name }
func (m *menuView) init() tea.Cmd            { return nil }
func (m *menuView) capturing() bool          { return true }
func (m *menuView) resize(width, height int) { m.width, m.height = width, height }
func (m *menuView) keys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "run")),
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move")),
		keyCancel,
	}
}

// choose closes the menu, then runs the item, so that a view it opens is
// pushed on top of the menu's parent.
func (m *menuView) choose(i int) tea.Cmd {
	return tea.Sequence(pop(m.vid), m.items[i].run())
}

func (m *menuView) update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "esc", "q", "a":
		return pop(m.vid)
	case "j", "down", "tab":
		m.cursor = (m.cursor + 1) % len(m.items)
		return nil
	case "k", "up", "shift+tab":
		m.cursor = (m.cursor - 1 + len(m.items)) % len(m.items)
		return nil
	case "enter":
		return m.choose(m.cursor)
	}
	for i, it := range m.items {
		if key.Matches(k, it.key) {
			return m.choose(i)
		}
	}
	return nil
}

func (m *menuView) render() string {
	t := m.env.theme
	keyWidth := 0
	for _, it := range m.items {
		keyWidth = max(keyWidth, len(it.key.Help().Key))
	}
	rows := []string{t.title.Render(m.name), ""}
	for i, it := range m.items {
		line := fmt.Sprintf("%-*s  %s", keyWidth, it.key.Help().Key, it.key.Help().Desc)
		if i == m.cursor {
			rows = append(rows, t.focus.Render("❯ "+line))
		} else {
			rows = append(rows, "  "+t.label.Render(line))
		}
	}
	rows = append(rows, "", t.dim.Render("enter run · esc close"))
	return centered(t, rows, m.width, m.height)
}
