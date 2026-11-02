package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// CollectionAdmin changes collections. A Backend may implement it; without
// it the browser is read-only.
type CollectionAdmin interface {
	CreateCollection(ctx context.Context, data api.Object) (api.Object, error)
	// UpdateCollection writes changes on top of the collection's properties.
	UpdateCollection(ctx context.Context, collection, changes api.Object) (api.Object, error)
	DeleteCollection(ctx context.Context, id string) error
	ReindexCollection(ctx context.Context, id string, flush bool) error
	ReingestCollection(ctx context.Context, id string, index bool) error
}

var (
	keyActions  = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "actions"))
	keyEdit     = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit"))
	keyDelete   = key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete"))
	keyReindex  = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "re-index"))
	keyReingest = key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "re-ingest"))
)

// categories are the dataset categories of OpenAleph. Investigations are
// collections of the category casefile; the server derives its casefile flag
// from that.
var categories = []string{
	"other", "news", "leak", "land", "gazette", "court", "company", "sanctions", "procurement", "finance",
	"grey", "library", "license", "regulatory", "poi", "customs", "census", "transport",
}

const casefile = "casefile"

func isInvestigation(c api.Object) bool { return str(c, "category") == casefile }

// addCollectionAdmin adds the create, edit, delete, re-index and re-ingest
// actions to the collections list, if the backend supports them.
func addCollectionAdmin(l *listView) {
	admin, ok := l.env.backend.(CollectionAdmin)
	if !ok {
		return
	}
	e := l.env
	newHelp := "new dataset"
	if l.investigations {
		newHelp = "new investigation"
	}
	keyNew := key.NewBinding(key.WithKeys("n"), key.WithHelp("n", newHelp))
	acts := []action{
		{key: keyNew, anyItem: true, run: func(l *listView, _ api.Object) tea.Cmd { return push(collectionForm(e, admin, l, nil)) }},
		{key: keyEdit, write: true, run: func(l *listView, c api.Object) tea.Cmd { return push(collectionForm(e, admin, l, c)) }},
		{key: keyReindex, write: true, run: func(_ *listView, c api.Object) tea.Cmd { return push(reindexDialog(e, admin, c)) }},
		{key: keyReingest, write: true, run: func(_ *listView, c api.Object) tea.Cmd { return push(reingestDialog(e, admin, c)) }},
		{key: keyDelete, write: true, run: func(l *listView, c api.Object) tea.Cmd { return push(deleteDialog(e, admin, l, c)) }},
	}
	menu := action{key: keyActions, anyItem: true, run: func(l *listView, c api.Object) tea.Cmd {
		var items []menuItem
		for _, a := range acts {
			if (c == nil && !a.anyItem) || (c != nil && a.write && !writeable(c)) {
				continue
			}
			items = append(items, menuItem{key: a.key, run: func() tea.Cmd { return a.run(l, c) }})
		}
		name := "Actions"
		if c != nil {
			name = str(c, "label")
			if !writeable(c) {
				name += " (read-only)"
			}
		}
		return push(newMenuView(e, name, items))
	}}
	l.actions = append(append([]action{menu}, l.actions...), acts...)
}

func collectionName(c api.Object) string {
	if s := str(c, "label"); s != "" {
		return s
	}
	return str(c, "id")
}

// collectionForm creates a collection (c nil) or edits c. On success the
// list shows the result without a reload, because the search index that
// lists collections may lag behind.
func collectionForm(e *env, admin CollectionAdmin, l *listView, c api.Object) *formView {
	var langs []string
	if vals, ok := c["languages"].([]any); ok {
		for _, v := range vals {
			if s, ok := v.(string); ok {
				langs = append(langs, s)
			}
		}
	}
	label := textInput("label", "Label", str(c, "label"), "e.g. Panama Papers")
	label.required = true
	foreignID := textInput("foreign_id", "Foreign ID", str(c, "foreign_id"), "empty: same as the label")
	foreignID.hint = "Unique key for scripts, for example my_leak"
	summary := textInput("summary", "Summary", str(c, "summary"), "")
	languages := textInput("languages", "Languages", strings.Join(langs, ", "), "e.g. en, de")
	languages.hint = "2-letter ISO 639 codes, separated by commas"
	fields := []*formField{label, foreignID, summary, languages}
	if !l.investigations {
		// Investigations always have the category casefile.
		cat := choiceInput("category", "Category", categories, str(c, "category"))
		fields = []*formField{label, foreignID, cat, summary, languages}
	}

	name := "New dataset"
	if l.investigations {
		name = "New investigation"
	}
	if c != nil {
		name = "Edit " + collectionName(c)
	}
	f := newFormView(e, name, fields)
	ctx, to := e.ctx, f.vid
	f.submit = func(v map[string]any) tea.Cmd {
		data := api.Object{
			"label": v["label"], "foreign_id": v["foreign_id"], "category": v["category"],
			"summary": v["summary"], "languages": splitList(v["languages"].(string)),
		}
		if l.investigations {
			data["category"] = casefile
		}
		if data["foreign_id"] == "" {
			data["foreign_id"] = data["label"]
		}
		return func() tea.Msg {
			var res api.Object
			var err error
			if c == nil {
				res, err = admin.CreateCollection(ctx, data)
			} else {
				res, err = admin.UpdateCollection(ctx, c, data)
			}
			return formDoneMsg{to: to, result: res, err: err}
		}
	}
	f.done = func(res api.Object) tea.Cmd {
		l.upsert(res)
		verb := "Created"
		if c != nil {
			verb = "Saved"
		}
		return tea.Batch(pop(f.vid), status(verb+" "+collectionName(res)))
	}
	return f
}

func splitList(s string) []string {
	out := []string{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, strings.ToLower(p))
	}
	return out
}

// collectionKey is what the user types to confirm destructive actions, like
// in `oa collection delete`.
func collectionKey(c api.Object) string {
	if s := str(c, "foreign_id"); s != "" {
		return s
	}
	return str(c, "id")
}

func deleteDialog(e *env, admin CollectionAdmin, l *listView, c api.Object) *confirmView {
	d := newConfirmView(e, "Delete "+collectionName(c),
		"This deletes the collection and everything in it.",
		"It cannot be undone.").typed(collectionKey(c))
	d.run = func(bool) tea.Cmd {
		ctx, id, to := e.ctx, str(c, "id"), d.vid
		return func() tea.Msg { return dialogDoneMsg{to: to, err: admin.DeleteCollection(ctx, id)} }
	}
	d.done = func() tea.Cmd {
		l.remove(str(c, "id"))
		return status("Deleted " + collectionName(c))
	}
	return d
}

func reindexDialog(e *env, admin CollectionAdmin, c api.Object) *confirmView {
	d := newConfirmView(e, "Re-index "+collectionName(c),
		"Re-index all entities of the collection.").withOption("Flush the index first", false)
	d.run = func(flush bool) tea.Cmd {
		ctx, id, to := e.ctx, str(c, "id"), d.vid
		return func() tea.Msg { return dialogDoneMsg{to: to, err: admin.ReindexCollection(ctx, id, flush)} }
	}
	d.done = func() tea.Cmd {
		return status(fmt.Sprintf("Re-index of %s started · p shows progress", collectionName(c)))
	}
	return d
}

func reingestDialog(e *env, admin CollectionAdmin, c api.Object) *confirmView {
	d := newConfirmView(e, "Re-ingest "+collectionName(c),
		"Process all documents of the collection again.").withOption("Index documents while processing", false)
	d.run = func(index bool) tea.Cmd {
		ctx, id, to := e.ctx, str(c, "id"), d.vid
		return func() tea.Msg { return dialogDoneMsg{to: to, err: admin.ReingestCollection(ctx, id, index)} }
	}
	d.done = func() tea.Cmd {
		return status(fmt.Sprintf("Re-ingest of %s started · p shows progress", collectionName(c)))
	}
	return d
}

// upsert replaces the item with the same ID, or adds it at the top.
func (l *listView) upsert(o api.Object) {
	id := str(o, "id")
	for i, it := range l.items {
		if str(it, "id") == id {
			l.items[i] = o
			l.syncRows()
			return
		}
	}
	l.items = append([]api.Object{o}, l.items...)
	l.total++
	l.syncRows()
	l.table.SetCursor(0)
}

func (l *listView) remove(id string) {
	for i, it := range l.items {
		if str(it, "id") == id {
			l.items = append(l.items[:i:i], l.items[i+1:]...)
			l.total = max(l.total-1, 0)
			l.syncRows()
			if c := l.table.Cursor(); c >= len(l.items) {
				l.table.SetCursor(max(len(l.items)-1, 0))
			}
			return
		}
	}
}
