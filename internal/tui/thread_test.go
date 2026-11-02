package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

func mail(id, date, from, subject string) api.Object {
	return api.Object{"id": id, "schema": "Email", "caption": subject,
		"collection": map[string]any{"id": "12", "label": "Leak A"},
		"properties": map[string]any{"date": []any{date}, "from": []any{from}, "subject": []any{subject}, "bodyText": []any{"text of " + id}}}
}

func TestAttachmentColumn(t *testing.T) {
	f := newReader()
	f.counts = map[string]int{"e1": 2, "e3": 1}
	f.entities = func(string, string, int) Page {
		return Page{Items: []api.Object{mail("e1", "2016-01-01", "a", "One"), mail("e2", "2016-01-02", "b", "Two"),
			mail("e3", "2016-01-03", "c", "Three")}, Total: 3}
	}
	s := start(t, Options{Backend: f, Collection: f.collections[0], Schemas: []string{"Email"}})
	if len(f.countCalls) != 1 || !slices.Equal(f.countCalls[0], []string{"e1", "e2", "e3"}) {
		t.Fatalf("count calls = %v", f.countCalls)
	}
	s.contains("ATT", "📎2", "📎1")
	for _, l := range strings.Split(s.screen(), "\n") {
		if strings.Contains(l, "Two") && strings.Contains(l, "📎") {
			t.Fatalf("e2 has no attachments: %q", l)
		}
	}
}

func TestNoAttachmentColumnWithoutEmails(t *testing.T) {
	f := newReader()
	s := start(t, Options{Backend: f, Collection: f.collections[0]})
	if strings.Contains(s.screen(), "ATT") || len(f.countCalls) != 0 {
		t.Fatalf("attachments column or request for a list without emails (%v)", f.countCalls)
	}
}

func TestThreadView(t *testing.T) {
	f := newReader()
	cur := mail("e2", "2016-01-02T09:30:00", "Bob <bob@x>", "Re: Offer")
	f.thread = []api.Object{mail("e1", "2016-01-01T08:00:00", "Jane <jane@x>", "Offer"), cur,
		mail("e3", "2016-01-03T10:00:00", "Jane <jane@x>", "Re: Re: Offer")}
	f.counts = map[string]int{"e1": 1}
	s := openDoc(t, f, cur)
	s.send(tea.WindowSizeMsg{Width: 100, Height: 24})
	foot := ansi.Strip(s.model().footer())
	if !strings.Contains(foot, "T thread") {
		t.Fatalf("footer = %q", foot)
	}
	s.keys("T")
	if f.threadOf != "e2" {
		t.Fatalf("thread of %q", f.threadOf)
	}
	s.contains("Re: Offer ❯ Thread", "2016-01-01 08:00", "Jane <jane@x>", "▶ Re: Offer", "Re: Re: Offer", "📎1", "1 of 3 emails")

	// / narrows by sender or subject.
	s.keys("/")
	s.typeText("bob")
	s.keys("enter")
	s.contains("1 of 1 emails", "▶ Re: Offer")

	// enter opens an email of the thread.
	s.keys("/")
	for range 3 {
		s.keys("backspace")
	}
	s.keys("enter", "G", "enter")
	s.contains("text of e3")
}
