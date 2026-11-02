package tui

import (
	"encoding/json"
	"fmt"
	"sort"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

type entityMsg struct {
	to     int
	entity api.Object
	err    error
}

func (m entityMsg) target() int { return m.to }

// field is one row of the entity view. next, if set, is the view that enter
// opens for the row; without it enter shows the full value.
type field struct {
	name, value string
	next        func() view
}

// entityView shows the properties of an entity, one value per row.
type entityView struct {
	env     *env
	vid     int
	entity  api.Object
	fields  []field
	table   table.Model
	loading bool
	width   int
}

// newEntityView shows e right away (search results carry the properties) and
// loads the full entity in the background.
func newEntityView(e *env, entity api.Object) *entityView {
	t := table.New(table.WithKeyMap(navKeys), table.WithStyles(e.theme.table), table.WithFocused(true))
	v := &entityView{env: e, vid: e.nextID(), entity: entity, table: t}
	v.setEntity(entity)
	return v
}

func (v *entityView) id() int { return v.vid }

func (v *entityView) title() string {
	if c := api.Caption(v.entity); c != "" {
		return c
	}
	return str(v.entity, "id")
}

func (v *entityView) capturing() bool { return false }

func (v *entityView) keys() []key.Binding {
	ks := []key.Binding{keyOpen, keyYank, keyWeb, keyJSON, keyReload}
	if isDocument(v.entity) {
		ks = append([]key.Binding{keyOpen, keyDownload}, ks[1:]...)
	}
	return ks
}

func (v *entityView) init() tea.Cmd { return v.load() }

func (v *entityView) load() tea.Cmd {
	v.loading = true
	ctx, b, id, to := v.env.ctx, v.env.backend, str(v.entity, "id"), v.vid
	return func() tea.Msg {
		e, err := b.Entity(ctx, id)
		return entityMsg{to: to, entity: e, err: err}
	}
}

func (v *entityView) setEntity(e api.Object) {
	v.entity = e
	v.fields = entityFields(v.env, e)
	rows := make([]table.Row, len(v.fields))
	prev := ""
	for i, f := range v.fields {
		name := f.name
		if name == prev {
			name = ""
		}
		prev = f.name
		value := oneLine(f.value)
		if f.next != nil {
			value += " ›"
		}
		rows[i] = table.Row{name, value}
	}
	// Columns first: the table renders rows against its columns.
	v.layout()
	v.table.SetRows(rows)
}

func entityFields(e *env, entity api.Object) []field {
	fields := []field{{name: "id", value: str(entity, "id")}, {name: "schema", value: str(entity, "schema")}}
	if c, ok := entity["collection"].(map[string]any); ok {
		fields = append(fields, field{name: "collection", value: str(c, "label"), next: func() view {
			return newEntitiesView(e, c, "", nil)
		}})
	}
	if isDocument(entity) {
		fields = append(fields, field{name: "contents", value: "files and attachments in it", next: func() view {
			c, _ := entity["collection"].(map[string]any)
			return newDocumentsView(e, c, entity)
		}})
	}
	props, _ := entity["properties"].(map[string]any)
	names := make([]string, 0, len(props))
	for k := range props {
		if k != "alephUrl" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		vals, _ := props[k].([]any)
		for _, val := range vals {
			switch val := val.(type) {
			case map[string]any:
				// Entity-valued properties come as nested entities.
				fields = append(fields, field{
					name:  k,
					value: fmt.Sprintf("%s [%s]", api.Caption(val), str(val, "schema")),
					next:  func() view { return newEntityView(e, val) },
				})
			case string:
				fields = append(fields, field{name: k, value: val})
			default:
				fields = append(fields, field{name: k, value: fmt.Sprint(val)})
			}
		}
	}
	return fields
}

func (v *entityView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case entityMsg:
		v.loading = false
		if msg.err != nil {
			return statusErr(msg.err)
		}
		cursor := v.table.Cursor()
		v.setEntity(msg.entity)
		v.table.SetCursor(min(cursor, max(len(v.fields)-1, 0)))
		return nil
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keyOpen):
			c := v.table.Cursor()
			if c < 0 || c >= len(v.fields) {
				return nil
			}
			f := v.fields[c]
			if f.next != nil {
				return push(f.next())
			}
			return push(newTextView(v.env, f.name, f.value))
		case key.Matches(msg, keyYank):
			return copyID(v.entity)
		case key.Matches(msg, keyWeb):
			return openURL(v.env, v.entity)
		case key.Matches(msg, keyJSON):
			b, err := json.MarshalIndent(v.entity, "", "  ")
			if err != nil {
				return statusErr(err)
			}
			return push(newTextView(v.env, "JSON", string(b)))
		case key.Matches(msg, keyReload):
			return v.load()
		case key.Matches(msg, keyDownload):
			return downloadDialog(v.env, v.entity)
		}
		var cmd tea.Cmd
		v.table, cmd = v.table.Update(msg)
		return cmd
	}
	return nil
}

func (v *entityView) resize(width, height int) {
	v.width = width
	// Columns and styles first: SetHeight subtracts the height of the
	// column titles.
	v.layout()
	v.table.SetStyles(v.env.theme.table)
	v.table.SetWidth(width)
	v.table.SetHeight(max(height-1, 2))
}

func (v *entityView) layout() {
	nameWidth := len("PROPERTY")
	for _, f := range v.fields {
		nameWidth = max(nameWidth, len(f.name))
	}
	nameWidth = min(nameWidth, 24)
	v.table.SetColumns([]table.Column{
		{Title: "PROPERTY", Width: nameWidth},
		{Title: "VALUE", Width: max(v.width-nameWidth-4, 8)},
	})
}

func (v *entityView) render() string {
	info := fmt.Sprintf("%s · %d values", str(v.entity, "schema"), len(v.fields))
	if v.loading {
		info += " · loading…"
	}
	return v.table.View() + "\n" + v.env.theme.dim.Render(truncate(" "+info, v.width))
}

// textView pages through a long value or the raw JSON of an entity.
type textView struct {
	env  *env
	vid  int
	name string
	text string
	vp   viewport.Model
}

func newTextView(e *env, name, text string) *textView {
	vp := viewport.New()
	vp.SoftWrap = true
	vp.KeyMap.PageDown = navKeys.PageDown
	vp.KeyMap.PageUp = navKeys.PageUp
	vp.KeyMap.HalfPageDown = navKeys.HalfPageDown
	vp.KeyMap.HalfPageUp = navKeys.HalfPageUp
	vp.SetContent(text)
	return &textView{env: e, vid: e.nextID(), name: name, text: text, vp: vp}
}

var keyCopyText = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy text"))

func (v *textView) id() int             { return v.vid }
func (v *textView) title() string       { return v.name }
func (v *textView) init() tea.Cmd       { return nil }
func (v *textView) capturing() bool     { return false }
func (v *textView) keys() []key.Binding { return []key.Binding{keyCopyText} }
func (v *textView) resize(width, h int) { v.vp.SetWidth(width); v.vp.SetHeight(max(h-1, 1)) }
func (v *textView) render() string      { return v.vp.View() + "\n" + v.info() }
func (v *textView) info() string {
	return v.env.theme.dim.Render(fmt.Sprintf(" %3.0f%%", v.vp.ScrollPercent()*100))
}
func (v *textView) update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(k, keyCopyText):
			return copyText(v.text, "Copied "+v.name)
		case key.Matches(k, navKeys.GotoTop):
			v.vp.GotoTop()
			return nil
		case key.Matches(k, navKeys.GotoBottom):
			v.vp.GotoBottom()
			return nil
		}
	}
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return cmd
}
