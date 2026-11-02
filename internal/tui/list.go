package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/dustin/go-humanize"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// loadAhead is how close to the end of the loaded rows the cursor may get
// before the next page is fetched.
const loadAhead = 10

// column describes a table column. A column with flex > 0 shares the width
// left over by the fixed columns.
type column struct {
	title string
	width int
	flex  int
	value func(api.Object) string
}

type pageMsg struct {
	to, gen, offset int
	page            Page
	err             error
}

func (m pageMsg) target() int { return m.to }

// action is a key that a list runs on the selected item. An action with
// anyItem also runs when the list is empty (item is then nil).
type action struct {
	key     key.Binding
	run     func(l *listView, item api.Object) tea.Cmd
	anyItem bool
	// write actions need write access to the item.
	write bool
}

// writeable reports whether the user may change o. Servers that do not send
// the flag get the benefit of the doubt; they still refuse on their own.
func writeable(o api.Object) bool {
	w, ok := o["writeable"].(bool)
	return !ok || w
}

// listView is a paginated, searchable table of API objects.
type listView struct {
	env     *env
	vid     int
	name    string
	noun    string
	fetch   func(ctx context.Context, q listQuery, offset int) (Page, error)
	cols    []column
	enter   func(api.Object) view
	actions []action
	// investigations marks the investigations list (for new collections).
	investigations bool
	height         int
	// hasEmails adds the attachments column; attach holds the counts.
	hasEmails bool
	attach    map[string]int
	// footerHide lists keys left out of the footer to keep it within 80
	// columns; ? shows them.
	footerHide map[string]bool
	// base and schemas select the entities of an entity list.
	base    EntityQuery
	schemas []string

	input   textinput.Model
	table   table.Model
	items   []api.Object
	query   string
	total   int
	atLeast bool
	more    bool
	loading bool
	gen     int
	width   int
}

func newListView(e *env, name, noun, query string) *listView {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "search"
	in.SetValue(query)
	t := table.New(table.WithKeyMap(navKeys), table.WithStyles(e.theme.table), table.WithFocused(true))
	return &listView{env: e, vid: e.nextID(), name: name, noun: noun, query: query, input: in, table: t, attach: map[string]int{}}
}

// newCollectionsView lists datasets or investigations.
func newCollectionsView(e *env, investigations bool) *listView {
	name, noun := "Datasets", "datasets"
	if investigations {
		name, noun = "Investigations", "investigations"
	}
	l := newListView(e, name, noun, "")
	l.investigations = investigations
	l.fetch = func(ctx context.Context, q listQuery, offset int) (Page, error) {
		return e.backend.Collections(ctx, q.text, investigations, offset)
	}
	l.cols = []column{
		{title: "ID", width: 6, value: func(o api.Object) string { return str(o, "id") }},
		{title: "LABEL", flex: 3, value: func(o api.Object) string { return str(o, "label") }},
		{title: "FOREIGN ID", flex: 1, value: func(o api.Object) string { return str(o, "foreign_id") }},
		{title: "CATEGORY", width: 12, value: func(o api.Object) string { return str(o, "category") }},
		{title: "ENTITIES", width: 10, value: func(o api.Object) string { return count(o["count"]) }},
	}
	if investigations {
		// All investigations have the category casefile.
		l.cols = append(l.cols[:3], l.cols[4])
	}
	l.enter = func(o api.Object) view { return newEntitiesView(e, o, "", nil) }
	l.actions = []action{
		{key: keyAll, anyItem: true, run: func(*listView, api.Object) tea.Cmd { return push(newEntitiesView(e, nil, "", nil)) }},
		{key: keyYank, run: yankID},
		{key: keyWeb, run: openUI},
	}
	addCollectionAdmin(l)
	return l
}

// newEntitiesView searches entities, in one collection or (collection nil) in all.
func newEntitiesView(e *env, collection api.Object, query string, schemas []string) *listView {
	name, collID := "Search", ""
	if collection != nil {
		name, collID = str(collection, "label"), str(collection, "id")
	}
	l := newListView(e, name, "entities", query)
	l.base, l.schemas = EntityQuery{CollectionID: collID}, schemas
	l.fetch = func(ctx context.Context, q listQuery, offset int) (Page, error) {
		return e.backend.Entities(ctx, EntityQuery{Query: q.text, CollectionID: collID, Schemas: q.schemas}, offset)
	}
	l.cols = []column{
		{title: "SCHEMA", width: 14, value: func(o api.Object) string { return str(o, "schema") }},
		{title: "NAME", flex: 3, value: api.Caption},
	}
	if collection == nil {
		l.cols = append(l.cols, column{title: "COLLECTION", flex: 1, value: func(o api.Object) string {
			c, _ := o["collection"].(map[string]any)
			return str(c, "label")
		}})
	}
	l.enter = func(o api.Object) view { return newEntityView(e, o) }
	l.actions = []action{
		{key: keySchema, anyItem: true, run: func(l *listView, _ api.Object) tea.Cmd { return push(newSchemaPicker(e, l)) }},
		{key: keyClearSchema, anyItem: true, run: func(l *listView, _ api.Object) tea.Cmd { return l.setSchemas(nil) }},
		{key: keyDownload, run: func(_ *listView, o api.Object) tea.Cmd { return downloadDialog(e, o) }},
		{key: keyYank, run: yankID},
		{key: keyWeb, run: openUI},
	}
	if collection != nil {
		pairViews(e, l, collection)
	}
	l.footerHide = map[string]bool{"enter": true, "d": true}
	return l
}

// listQuery is what a list was loaded with.
type listQuery struct {
	text    string
	schemas []string
}

// setSchemas filters an entity list by schemas (nil: no filter) and reloads it.
func (l *listView) setSchemas(schemas []string) tea.Cmd {
	if len(schemas) == 0 && len(l.schemas) == 0 {
		return nil
	}
	l.schemas = schemas
	return l.reload()
}

func (l *listView) id() int { return l.vid }

func (l *listView) footerHidden() map[string]bool { return l.footerHide }

func (l *listView) title() string {
	t := l.name
	if l.query != "" {
		t += fmt.Sprintf(" %q", l.query)
	}
	if len(l.schemas) > 0 {
		t += " [" + strings.Join(l.schemas, ", ") + "]"
	}
	return t
}

func (l *listView) init() tea.Cmd { return l.reload() }

func (l *listView) capturing() bool { return l.input.Focused() }

func (l *listView) keys() []key.Binding {
	ks := []key.Binding{keyOpen, keySearch}
	for _, a := range l.actions {
		ks = append(ks, a.key)
	}
	return append(ks, keyReload)
}

func (l *listView) selected() api.Object {
	if c := l.table.Cursor(); c >= 0 && c < len(l.items) {
		return l.items[c]
	}
	return nil
}

func (l *listView) reload() tea.Cmd {
	l.gen++
	l.items, l.total, l.more = nil, 0, false
	l.attach = map[string]int{}
	l.table.SetRows(nil)
	l.table.GotoTop()
	return l.load(0)
}

func (l *listView) load(offset int) tea.Cmd {
	l.loading = true
	// Copy the query: the command runs in another goroutine.
	q := listQuery{text: l.query, schemas: append([]string(nil), l.schemas...)}
	ctx, fetch, to, gen := l.env.ctx, l.fetch, l.vid, l.gen
	return func() tea.Msg {
		p, err := fetch(ctx, q, offset)
		return pageMsg{to: to, gen: gen, offset: offset, page: cleanPage(p), err: err}
	}
}

// capped reports whether the list reached the server's paging limit.
func (l *listView) capped() bool { return len(l.items) >= MaxResults }

// loadMore fetches the next page when the cursor nears the end.
func (l *listView) loadMore() tea.Cmd {
	if l.loading || !l.more || l.capped() || l.table.Cursor() < len(l.items)-loadAhead {
		return nil
	}
	return l.load(len(l.items))
}

func (l *listView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case pageMsg:
		if msg.gen != l.gen || msg.offset != len(l.items) {
			return nil
		}
		l.loading = false
		if msg.err != nil {
			return statusErr(msg.err)
		}
		l.items = append(l.items, msg.page.Items...)
		l.total, l.atLeast = msg.page.Total, msg.page.TotalAtLeast
		l.more = msg.page.More && len(msg.page.Items) > 0
		if !l.hasEmails && slices.ContainsFunc(msg.page.Items, isEmail) {
			// The attachments column appears with the first email.
			l.hasEmails = true
			l.resize(l.width, l.height)
		}
		l.syncRows()
		return tea.Batch(l.loadMore(), l.countAttachments(msg.page.Items))
	case attachCountsMsg:
		if msg.gen != l.gen {
			return nil
		}
		for id, n := range msg.counts {
			l.attach[id] = n
		}
		l.syncRows()
		return nil
	case tea.KeyPressMsg:
		if l.input.Focused() {
			switch msg.String() {
			case "enter":
				l.input.Blur()
				l.query = strings.TrimSpace(l.input.Value())
				return l.reload()
			case "esc":
				l.input.SetValue(l.query)
				l.input.Blur()
				return nil
			}
			var cmd tea.Cmd
			l.input, cmd = l.input.Update(msg)
			return cmd
		}
		switch {
		case key.Matches(msg, keySearch):
			l.input.CursorEnd()
			return l.input.Focus()
		case key.Matches(msg, keyReload):
			return l.reload()
		case key.Matches(msg, keyOpen):
			if it := l.selected(); it != nil && l.enter != nil {
				return push(l.enter(it))
			}
			return nil
		}
		for _, a := range l.actions {
			if key.Matches(msg, a.key) {
				it := l.selected()
				switch {
				case it == nil && !a.anyItem:
					return nil
				case a.write && !writeable(it):
					return statusErr(fmt.Errorf("you cannot change %s: no write access", str(it, "label")))
				}
				return a.run(l, it)
			}
		}
		var cmd tea.Cmd
		l.table, cmd = l.table.Update(msg)
		return tea.Batch(cmd, l.loadMore())
	}
	if l.input.Focused() {
		var cmd tea.Cmd
		l.input, cmd = l.input.Update(msg)
		return cmd
	}
	return nil
}

func (l *listView) syncRows() {
	rows := make([]table.Row, len(l.items))
	for i, it := range l.items {
		cols := l.columns()
		row := make(table.Row, len(cols))
		for j, c := range cols {
			row[j] = oneLine(c.value(it))
		}
		rows[i] = row
	}
	l.table.SetRows(rows)
	// The cursor of an empty table is -1.
	if l.table.Cursor() < 0 && len(rows) > 0 {
		l.table.SetCursor(0)
	}
}

func (l *listView) resize(width, height int) {
	l.width, l.height = width, height
	l.input.SetWidth(max(width-4, 1))
	// Columns and styles first: SetHeight subtracts the height of the
	// column titles. The input line and the count line take one row each.
	l.table.SetColumns(layoutColumns(l.columns(), width))
	l.table.SetStyles(l.env.theme.table)
	l.table.SetWidth(width)
	l.table.SetHeight(max(height-2, 2))
}

// layoutColumns gives flex columns the width left over by fixed columns. Each
// cell is padded by one space on both sides.
func layoutColumns(cols []column, width int) []table.Column {
	free, flex := width, 0
	for _, c := range cols {
		free -= 2
		if c.flex > 0 {
			flex += c.flex
		} else {
			free -= c.width
		}
	}
	out := make([]table.Column, len(cols))
	for i, c := range cols {
		w := c.width
		if c.flex > 0 {
			w = max(free*c.flex/flex, 8)
		}
		out[i] = table.Column{Title: c.title, Width: w}
	}
	return out
}

func (l *listView) render() string {
	input := " " + l.input.View()
	if !l.input.Focused() && l.query == "" {
		input = l.env.theme.dim.Render(" press / to search " + l.noun)
	}
	var info string
	switch {
	case l.loading && len(l.items) == 0:
		info = "loading…"
	case len(l.items) == 0:
		info = "no " + l.noun
	default:
		total := humanize.Comma(int64(l.total))
		if l.atLeast {
			total += "+"
		}
		info = fmt.Sprintf("%d of %s %s", l.table.Cursor()+1, total, l.noun)
		if l.loading {
			info += " · loading…"
		}
		if l.capped() && l.total > len(l.items) {
			info += fmt.Sprintf(" · only the first %s can be listed, narrow the search", humanize.Comma(MaxResults))
		}
	}
	if len(l.schemas) > 0 {
		info += " · schema: " + strings.Join(l.schemas, ", ") + " (F clears)"
	}
	return strings.Join([]string{input, l.table.View(), l.env.theme.dim.Render(truncate(" "+info, l.width))}, "\n")
}

func yankID(_ *listView, it api.Object) tea.Cmd { return copyID(it) }

func openUI(l *listView, it api.Object) tea.Cmd { return openURL(l.env, it) }

// copyText copies s to the clipboard of the terminal (OSC 52), which also works
// over SSH, and shows done in the status line.
func copyText(s, done string) tea.Cmd {
	if s == "" {
		return nil
	}
	return tea.Batch(tea.SetClipboard(s), status(done))
}

func copyID(o api.Object) tea.Cmd {
	id := str(o, "id")
	return copyText(id, "Copied ID "+id)
}

// openURL opens the web UI page of an entity or collection.
func openURL(e *env, o api.Object) tea.Cmd {
	links, _ := o["links"].(map[string]any)
	u, _ := links["ui"].(string)
	if u == "" {
		return statusErr(fmt.Errorf("no web link for %s", str(o, "id")))
	}
	return func() tea.Msg {
		if err := e.open(u); err != nil {
			return statusMsg{text: err.Error(), err: true}
		}
		return statusMsg{text: "Opened " + u}
	}
}

// str returns a string field of an API object.
func str(o api.Object, k string) string {
	switch v := o[k].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case bool:
		return fmt.Sprint(v)
	}
	return ""
}

func count(v any) string {
	if f, ok := v.(float64); ok {
		return humanize.Comma(int64(f))
	}
	return ""
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func isEmail(o api.Object) bool {
	s := str(o, "schema")
	return s == "Email" || s == "Message"
}

type attachCountsMsg struct {
	to, gen int
	counts  map[string]int
}

func (m attachCountsMsg) target() int { return m.to }

// columns are the columns of the list, with the attachments column after
// the name once the list shows emails.
func (l *listView) columns() []column {
	if !l.hasEmails {
		return l.cols
	}
	att := column{title: "ATT", width: 4, value: func(o api.Object) string {
		if n := l.attach[str(o, "id")]; n > 0 {
			return fmt.Sprintf("📎%d", n)
		}
		return ""
	}}
	cols := make([]column, 0, len(l.cols)+1)
	for _, c := range l.cols {
		cols = append(cols, c)
		if c.title == "NAME" {
			cols = append(cols, att)
		}
	}
	return cols
}

// countAttachments asks for the attachment counts of the emails in items,
// with one request for the page.
func (l *listView) countAttachments(items []api.Object) tea.Cmd {
	reader, ok := l.env.backend.(DocumentReader)
	if !ok {
		return nil
	}
	var ids []string
	for _, it := range items {
		if isEmail(it) {
			ids = append(ids, str(it, "id"))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	ctx, to, gen := l.env.ctx, l.vid, l.gen
	return func() tea.Msg {
		counts, err := reader.AttachmentCounts(ctx, ids)
		if err != nil {
			// Without counts the list still works; say why they are missing.
			return statusMsg{text: "attachments: " + err.Error(), err: true}
		}
		return attachCountsMsg{to: to, gen: gen, counts: counts}
	}
}
