package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/dustin/go-humanize"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

type schemaCountsMsg struct {
	to     int
	values []api.FacetValue
	err    error
}

func (m schemaCountsMsg) target() int { return m.to }

// schemaPicker filters an entity list by schema. It shows the schemas that
// occur in the list with their counts. Typing narrows the choices, space
// selects several (they combine with OR), enter applies.
type schemaPicker struct {
	env      *env
	vid      int
	list     *listView
	input    textinput.Model
	values   []api.FacetValue
	selected map[string]bool
	cursor   int // 0 is "all schemas", then the visible values
	loading  bool
	err      string
	width    int
	height   int
}

func newSchemaPicker(e *env, l *listView) *schemaPicker {
	in := textinput.New()
	in.Prompt = "❯ "
	in.Placeholder = "type to narrow"
	sel := map[string]bool{}
	for _, s := range l.schemas {
		sel[s] = true
	}
	return &schemaPicker{env: e, vid: e.nextID(), list: l, input: in, selected: sel, loading: true}
}

func (p *schemaPicker) id() int         { return p.vid }
func (p *schemaPicker) title() string   { return "Schema" }
func (p *schemaPicker) capturing() bool { return true }

func (p *schemaPicker) keys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "select")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
		keyCancel,
	}
}

func (p *schemaPicker) init() tea.Cmd {
	ctx, b, to := p.env.ctx, p.env.backend, p.vid
	q := EntityQuery{Query: p.list.query, CollectionID: p.list.collID}
	return tea.Batch(p.input.Focus(), func() tea.Msg {
		vals, err := b.SchemaCounts(ctx, q)
		return schemaCountsMsg{to: to, values: vals, err: err}
	})
}

func (p *schemaPicker) resize(width, height int) {
	p.width, p.height = width, height
	p.input.SetWidth(max(min(width-12, 40), 10))
}

// visible returns the values matching the typed text. Active filters that
// no longer occur are kept, so they can be deselected.
func (p *schemaPicker) visible() []api.FacetValue {
	vals := slices.Clone(p.values)
	for s := range p.selected {
		if !slices.ContainsFunc(vals, func(v api.FacetValue) bool { return v.ID == s }) {
			vals = append(vals, api.FacetValue{ID: s, Label: s})
		}
	}
	text := strings.ToLower(strings.TrimSpace(p.input.Value()))
	if text == "" {
		return vals
	}
	var out []api.FacetValue
	for _, v := range vals {
		if strings.Contains(strings.ToLower(v.ID), text) || strings.Contains(strings.ToLower(v.Label), text) {
			out = append(out, v)
		}
	}
	return out
}

func (p *schemaPicker) apply() tea.Cmd {
	var schemas []string
	vis := p.visible()
	switch {
	case len(p.selected) > 0:
		for s := range p.selected {
			schemas = append(schemas, s)
		}
		slices.Sort(schemas)
	case p.cursor > 0 && p.cursor <= len(vis):
		schemas = []string{vis[p.cursor-1].ID}
	}
	return tea.Batch(pop(p.vid), p.list.setSchemas(schemas))
}

func (p *schemaPicker) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case schemaCountsMsg:
		p.loading = false
		if msg.err != nil {
			p.err = msg.err.Error()
			return nil
		}
		p.values = msg.values
		return nil
	case tea.KeyPressMsg:
		vis := p.visible()
		switch msg.String() {
		case "esc":
			return pop(p.vid)
		case "enter":
			return p.apply()
		case "down", "ctrl+n", "tab":
			p.cursor = min(p.cursor+1, len(vis))
			return nil
		case "up", "ctrl+p", "shift+tab":
			p.cursor = max(p.cursor-1, 0)
			return nil
		case "space":
			if p.cursor == 0 {
				clear(p.selected)
			} else if p.cursor <= len(vis) {
				id := vis[p.cursor-1].ID
				if p.selected[id] {
					delete(p.selected, id)
				} else {
					p.selected[id] = true
				}
			}
			return nil
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		// Typing narrows the list: start at the first match.
		if vis := p.visible(); len(vis) > 0 && strings.TrimSpace(p.input.Value()) != "" {
			p.cursor = 1
		} else {
			p.cursor = min(p.cursor, len(vis))
		}
		return cmd
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

func (p *schemaPicker) render() string {
	t := p.env.theme
	vis := p.visible()
	idWidth, labelWidth := len("All schemas"), 0
	for _, v := range vis {
		idWidth = max(idWidth, len(v.ID))
		if v.Label != v.ID {
			labelWidth = max(labelWidth, len(v.Label))
		}
	}
	line := func(i int, check, id, label, count string) string {
		row := fmt.Sprintf("%s %-*s  %-*s  %7s", check, idWidth, id, labelWidth, label, count)
		if i == p.cursor {
			return t.focus.Render("❯ " + row)
		}
		return "  " + row
	}
	allCheck := "( )"
	if len(p.selected) == 0 {
		allCheck = "(•)"
	}
	rows := []string{line(0, allCheck, "All schemas", "", "")}
	for i, v := range vis {
		check := "[ ]"
		if p.selected[v.ID] {
			check = "[x]"
		}
		label := v.Label
		if label == v.ID {
			label = ""
		}
		rows = append(rows, line(i+1, check, v.ID, label, humanize.Comma(int64(v.Count))))
	}

	// Show a window of rows around the cursor that fits the dialog.
	maxRows := max(p.height-12, 3)
	start := min(max(p.cursor-maxRows/2, 0), max(len(rows)-maxRows, 0))
	end := min(start+maxRows, len(rows))
	body := []string{t.title.Render("Filter " + p.list.name + " by schema"), "", p.input.View(), ""}
	body = append(body, rows[start:end]...)
	if end < len(rows) {
		body = append(body, t.dim.Render(fmt.Sprintf("  … %d more", len(rows)-end)))
	}
	body = append(body, "")
	switch {
	case p.loading:
		body = append(body, t.dim.Render("counting…"))
	case p.err != "":
		body = append(body, t.err.Render(p.err))
	case len(p.values) == 0:
		body = append(body, t.dim.Render("no entities"))
	default:
		body = append(body, t.dim.Render("space select · enter apply · esc cancel"))
	}
	return centered(t, body, p.width, p.height)
}
