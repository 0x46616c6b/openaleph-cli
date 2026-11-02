package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

// defaultStatusInterval is how often the status view refreshes.
const defaultStatusInterval = 3 * time.Second

const progressWidth = 12

type statusRowsMsg struct {
	to   int
	rows []api.Object
	at   time.Time
	err  error
}

func (m statusRowsMsg) target() int { return m.to }

type statusTickMsg struct{ to int }

func (m statusTickMsg) target() int { return m.to }

// statusView shows the collections that are being processed and refreshes
// itself while it is on the stack.
type statusView struct {
	env      *env
	vid      int
	interval time.Duration
	table    table.Model
	rows     []api.Object
	updated  time.Time
	loading  bool
	stale    bool
	width    int
}

// resumed refreshes right away if a refresh was skipped while covered.
func (v *statusView) resumed() tea.Cmd {
	if !v.stale {
		return nil
	}
	v.stale = false
	return v.load()
}

func newStatusView(e *env) *statusView {
	t := table.New(table.WithKeyMap(navKeys), table.WithStyles(e.theme.table), table.WithFocused(true))
	return &statusView{env: e, vid: e.nextID(), interval: e.statusInterval, table: t}
}

func (v *statusView) id() int             { return v.vid }
func (v *statusView) title() string       { return "Status" }
func (v *statusView) capturing() bool     { return false }
func (v *statusView) init() tea.Cmd       { return tea.Batch(v.load(), v.tick()) }
func (v *statusView) keys() []key.Binding { return []key.Binding{keyOpen, keyYank, keyWeb, keyReload} }

func (v *statusView) tick() tea.Cmd {
	to := v.vid
	return tea.Tick(v.interval, func(time.Time) tea.Msg { return statusTickMsg{to} })
}

func (v *statusView) load() tea.Cmd {
	v.loading = true
	ctx, b, to := v.env.ctx, v.env.backend, v.vid
	return func() tea.Msg {
		rows, err := b.Status(ctx)
		for _, r := range rows {
			cleanValue(r)
		}
		return statusRowsMsg{to: to, rows: rows, at: time.Now(), err: err}
	}
}

func (v *statusView) selected() api.Object {
	if c := v.table.Cursor(); c >= 0 && c < len(v.rows) {
		return statusCollection(v.rows[c])
	}
	return nil
}

func (v *statusView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case statusTickMsg:
		// Popped views never get the tick, which ends the refresh loop.
		// Covered views skip the request and refresh when shown again.
		if v.env.topID != v.vid {
			v.stale = true
			return v.tick()
		}
		return tea.Batch(v.load(), v.tick())
	case statusRowsMsg:
		v.loading = false
		if msg.err != nil {
			return statusErr(msg.err)
		}
		v.setRows(msg.rows)
		v.updated = msg.at
		return nil
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keyReload):
			return v.load()
		case key.Matches(msg, keyOpen):
			if c := v.selected(); c != nil {
				return push(newEntitiesView(v.env, c, "", nil))
			}
			return nil
		case key.Matches(msg, keyYank):
			if c := v.selected(); c != nil {
				return copyID(c)
			}
			return nil
		case key.Matches(msg, keyWeb):
			if c := v.selected(); c != nil {
				return openURL(v.env, c)
			}
			return nil
		}
		var cmd tea.Cmd
		v.table, cmd = v.table.Update(msg)
		return cmd
	}
	return nil
}

// setRows replaces the rows and keeps the cursor on the same collection.
func (v *statusView) setRows(rows []api.Object) {
	var current string
	if c := v.selected(); c != nil {
		current = str(c, "id")
	}
	v.rows = rows
	out := make([]table.Row, len(rows))
	cursor := 0
	for i, r := range rows {
		c := statusCollection(r)
		if str(c, "id") == current {
			cursor = i
		}
		todo, doing, failed, done := num(r, "todo"), num(r, "doing"), num(r, "failed"), num(r, "finished")
		ratio := 0.0
		if all := todo + doing + failed + done; all > 0 {
			ratio = (done + failed) / all
		}
		out[i] = table.Row{
			str(c, "id"), oneLine(str(c, "label")), output.ProgressBar(ratio, progressWidth),
			count(r["todo"]), count(r["doing"]), count(r["failed"]), count(r["finished"]),
			output.Duration(str(r, "remaining_time")),
		}
	}
	v.table.SetRows(out)
	if len(out) > 0 {
		v.table.SetCursor(cursor)
	}
}

func (v *statusView) resize(width, height int) {
	v.width = width
	// Columns and styles first: rows render against the columns, and
	// SetHeight subtracts the height of the column titles. The widths fit
	// 80 columns.
	v.table.SetColumns(layoutColumns([]column{
		{title: "ID", width: 5},
		{title: "COLLECTION", flex: 1},
		{title: "PROGRESS", width: progressWidth + 5},
		{title: "TODO", width: 6},
		{title: "DOING", width: 6},
		{title: "FAILED", width: 6},
		{title: "DONE", width: 7},
		{title: "ETA", width: 6},
	}, width))
	v.table.SetStyles(v.env.theme.table)
	v.table.SetWidth(width)
	v.table.SetHeight(max(height-1, 2))
}

func (v *statusView) render() string {
	var info string
	switch {
	case v.updated.IsZero():
		info = "loading…"
	case len(v.rows) == 0:
		info = "no collections are being processed"
	default:
		var done, total float64
		for _, r := range v.rows {
			d := num(r, "finished") + num(r, "failed")
			done += d
			total += d + num(r, "todo") + num(r, "doing")
		}
		pct := 0.0
		if total > 0 {
			pct = done / total * 100
		}
		info = fmt.Sprintf("%d collections · %.0f%% overall", len(v.rows), pct)
	}
	if !v.updated.IsZero() {
		info += fmt.Sprintf(" · updated %s · every %s", v.updated.Format("15:04:05"), v.interval)
	}
	return v.table.View() + "\n" + v.env.theme.dim.Render(truncate(" "+info, v.width))
}

// statusCollection returns the collection of a /status row. Older servers
// only send collection_id and name.
func statusCollection(r api.Object) api.Object {
	if c, ok := r["collection"].(map[string]any); ok && c["id"] != nil {
		return c
	}
	return api.Object{"id": r["collection_id"], "label": r["name"]}
}

func num(o api.Object, k string) float64 {
	f, _ := o[k].(float64)
	return f
}
