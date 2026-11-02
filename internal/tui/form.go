package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

type fieldKind int

const (
	textField fieldKind = iota
	choiceField
)

// formField is one row of a form: a text input or a choice from a list.
type formField struct {
	key, label, hint string
	kind             fieldKind
	required         bool
	input            textinput.Model
	choices          []string
	choice           int
}

func textInput(key, label, value, placeholder string) *formField {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.SetValue(value)
	return &formField{key: key, label: label, kind: textField, input: in}
}

func choiceInput(key, label string, choices []string, value string) *formField {
	f := &formField{key: key, label: label, kind: choiceField, choices: choices}
	for i, c := range choices {
		if c == value {
			f.choice = i
			return f
		}
	}
	if value != "" {
		// Keep a value the server knows but this list does not.
		f.choices = append(f.choices, value)
		f.choice = len(f.choices) - 1
	}
	return f
}

func (f *formField) value() any {
	if f.kind == choiceField {
		return f.choices[f.choice]
	}
	return strings.TrimSpace(f.input.Value())
}

type formDoneMsg struct {
	to     int
	result api.Object
	err    error
}

func (m formDoneMsg) target() int { return m.to }

var (
	keyNextField = key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next field"))
	keyPrevField = key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("⇧tab", "previous"))
	keySave      = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^s", "save"))
	keyCancel    = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	keyToggle    = key.NewBinding(key.WithKeys("space", "left", "right"), key.WithHelp("space/←→", "change"))
)

// formView edits a set of fields and saves them through submit. On success
// done decides what happens next (usually: close the form and refresh).
type formView struct {
	env    *env
	vid    int
	name   string
	fields []*formField
	focus  int
	saving bool
	err    string
	width  int
	height int
	submit func(values map[string]any) tea.Cmd
	done   func(result api.Object) tea.Cmd
}

func newFormView(e *env, name string, fields []*formField) *formView {
	f := &formView{env: e, vid: e.nextID(), name: name, fields: fields}
	f.setFocus(0)
	return f
}

func (f *formView) id() int         { return f.vid }
func (f *formView) title() string   { return f.name }
func (f *formView) init() tea.Cmd   { return textinput.Blink }
func (f *formView) capturing() bool { return true }
func (f *formView) keys() []key.Binding {
	return []key.Binding{keyNextField, keyPrevField, keyToggle, keySave, keyCancel}
}

func (f *formView) resize(width, height int) {
	f.width, f.height = width, height
	for _, fl := range f.fields {
		fl.input.SetWidth(max(min(width-24, 60), 10))
	}
}

func (f *formView) setFocus(i int) tea.Cmd {
	f.focus = (i + len(f.fields)) % len(f.fields)
	var cmd tea.Cmd
	for j, fl := range f.fields {
		if fl.kind != textField {
			continue
		}
		if j == f.focus {
			cmd = fl.input.Focus()
		} else {
			fl.input.Blur()
		}
	}
	return cmd
}

func (f *formView) values() map[string]any {
	v := make(map[string]any, len(f.fields))
	for _, fl := range f.fields {
		v[fl.key] = fl.value()
	}
	return v
}

func (f *formView) save() tea.Cmd {
	if f.saving {
		return nil
	}
	for i, fl := range f.fields {
		if fl.required && fl.value() == "" {
			f.err = fl.label + " is required"
			return f.setFocus(i)
		}
	}
	f.saving, f.err = true, ""
	return f.submit(f.values())
}

func (f *formView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case formDoneMsg:
		f.saving = false
		if msg.err != nil {
			// Stay in the form so the input can be fixed.
			f.err = msg.err.Error()
			return nil
		}
		return f.done(msg.result)
	case tea.KeyPressMsg:
		fl := f.fields[f.focus]
		switch {
		case key.Matches(msg, keyCancel):
			return pop(f.vid)
		case key.Matches(msg, keySave):
			return f.save()
		case msg.String() == "enter":
			if f.focus == len(f.fields)-1 {
				return f.save()
			}
			return f.setFocus(f.focus + 1)
		case key.Matches(msg, keyNextField):
			return f.setFocus(f.focus + 1)
		case key.Matches(msg, keyPrevField):
			return f.setFocus(f.focus - 1)
		}
		switch fl.kind {
		case choiceField:
			switch msg.String() {
			case "left", "h":
				fl.choice = (fl.choice - 1 + len(fl.choices)) % len(fl.choices)
			case "right", "l", "space":
				fl.choice = (fl.choice + 1) % len(fl.choices)
			}
			return nil
		}
		var cmd tea.Cmd
		fl.input, cmd = fl.input.Update(msg)
		return cmd
	}
	var cmd tea.Cmd
	if fl := f.fields[f.focus]; fl.kind == textField {
		fl.input, cmd = fl.input.Update(msg)
	}
	return cmd
}

func (f *formView) render() string {
	t := f.env.theme
	labelWidth := 0
	for _, fl := range f.fields {
		labelWidth = max(labelWidth, len(fl.label))
	}
	rows := []string{t.title.Render(f.name), ""}
	for i, fl := range f.fields {
		ls := t.label
		marker := "  "
		if i == f.focus {
			ls, marker = t.focus, t.focus.Render("❯ ")
		}
		var control string
		switch fl.kind {
		case textField:
			control = fl.input.View()
		case choiceField:
			// Only the focused choice gets the padded chip, so values line up.
			control = fl.choices[fl.choice]
			if i == f.focus {
				control = t.choice.Render(control) + t.dim.Render("  ←/→")
			}
		}
		rows = append(rows, marker+ls.Render(fmt.Sprintf("%-*s", labelWidth, fl.label))+"  "+control)
		if i == f.focus && fl.hint != "" {
			rows = append(rows, "  "+strings.Repeat(" ", labelWidth+2)+t.dim.Render(fl.hint))
		}
	}
	rows = append(rows, "")
	switch {
	case f.saving:
		rows = append(rows, t.dim.Render("saving…"))
	case f.err != "":
		rows = append(rows, t.err.Render(f.err))
	default:
		rows = append(rows, t.dim.Render("enter next · ctrl+s save · esc cancel"))
	}
	return centered(t, rows, f.width, f.height)
}

// centered draws rows in a dialog box in the middle of the area.
func centered(t *theme, rows []string, width, height int) string {
	box := t.dialog.MaxWidth(width).Render(strings.Join(rows, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
