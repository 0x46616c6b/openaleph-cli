package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

var keyThread = key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "thread"))

// newThreadView lists the emails of the thread of email, oldest first, with
// email itself marked. / narrows the list by sender or subject.
func newThreadView(e *env, reader DocumentReader, email api.Object) *listView {
	l := newListView(e, "Thread", "emails", "")
	current := str(email, "id")
	l.fetch = func(ctx context.Context, q listQuery, offset int) (Page, error) {
		if offset > 0 {
			return Page{}, nil
		}
		all, err := reader.Thread(ctx, email)
		if err != nil {
			return Page{}, err
		}
		text := strings.ToLower(q.text)
		var items []api.Object
		for _, m := range all {
			if text == "" || strings.Contains(strings.ToLower(firstProp(m, "from")+" "+api.Caption(m)), text) {
				items = append(items, m)
			}
		}
		return Page{Items: items, Total: len(items)}, nil
	}
	l.cols = []column{
		{title: "DATE", width: 16, value: func(o api.Object) string {
			// Up to the minute: 2016-01-02 09:30.
			d := strings.Replace(firstProp(o, "date"), "T", " ", 1)
			return d[:min(len(d), 16)]
		}},
		{title: "FROM", flex: 1, value: func(o api.Object) string { return firstProp(o, "from") }},
		{title: "NAME", flex: 2, value: func(o api.Object) string {
			if str(o, "id") == current {
				return "▶ " + api.Caption(o)
			}
			return api.Caption(o)
		}},
	}
	l.enter = func(o api.Object) view { return newEntityView(e, o) }
	l.actions = []action{
		{key: keyDownload, run: func(_ *listView, o api.Object) tea.Cmd { return downloadDialog(e, o) }},
		{key: keyYank, run: yankID},
		{key: keyWeb, run: openUI},
	}
	l.footerHide = map[string]bool{"enter": true}
	return l
}
