package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

type entityCall struct {
	query, collection string
	schemas           []string
	offset            int
	parent            string
	topLevel          bool
}

type fakeBackend struct {
	mu          sync.Mutex
	collections []api.Object
	entities    func(q, coll string, offset int) Page
	entity      map[string]api.Object
	err         error
	calls       []entityCall
	status      [][]api.Object // one result per Status call; the last repeats
	statusCalls int

	schemaCounts []api.FacetValue
	// docs answers folder queries (Parent or TopLevel).
	docs         func(q EntityQuery) Page
	facetQueries []EntityQuery
}

func (f *fakeBackend) Status(context.Context) ([]api.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.statusCalls++
	if len(f.status) == 0 {
		return nil, nil
	}
	return f.status[min(f.statusCalls, len(f.status))-1], nil
}

func (f *fakeBackend) Collections(_ context.Context, q string, investigations bool, offset int) (Page, error) {
	if f.err != nil {
		return Page{}, f.err
	}
	var items []api.Object
	for _, c := range f.collections {
		if isInvestigation(c) == investigations {
			items = append(items, c)
		}
	}
	return Page{Items: items, Total: len(items)}, nil
}

func (f *fakeBackend) Entities(_ context.Context, q EntityQuery, offset int) (Page, error) {
	f.mu.Lock()
	f.calls = append(f.calls, entityCall{q.Query, q.CollectionID, q.Schemas, offset, q.Parent, q.TopLevel})
	f.mu.Unlock()
	if q.Parent != "" || q.TopLevel {
		if f.docs == nil {
			return Page{}, nil
		}
		return f.docs(q), nil
	}
	return f.entities(q.Query, q.CollectionID, offset), nil
}

func (f *fakeBackend) SchemaCounts(_ context.Context, q EntityQuery) ([]api.FacetValue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.facetQueries = append(f.facetQueries, q)
	return f.schemaCounts, nil
}

func (f *fakeBackend) Entity(_ context.Context, id string) (api.Object, error) {
	if e, ok := f.entity[id]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("entity %s not found", id)
}

func (f *fakeBackend) lastCall() entityCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func newFake() *fakeBackend {
	company := api.Object{
		"id": "c1", "schema": "Company", "caption": "Acme Corp",
		"collection": map[string]any{"id": "12", "label": "Leak A"},
		"links":      map[string]any{"ui": "https://aleph.example.org/entities/c1"},
		"properties": map[string]any{
			"name":         []any{"Acme Corp"},
			"jurisdiction": []any{"pa"},
			"notes":        []any{"line one\nline two"},
		},
	}
	person := api.Object{
		"id": "p1", "schema": "Person", "caption": "Jane Doe",
		"properties": map[string]any{"name": []any{"Jane Doe"}},
	}
	ownership := api.Object{
		"id": "o1", "schema": "Ownership", "caption": "Ownership",
		"properties": map[string]any{"owner": []any{person}, "asset": []any{company}},
	}
	return &fakeBackend{
		collections: []api.Object{
			{"id": "12", "label": "Leak A", "foreign_id": "leak_a", "category": "leak", "count": 48210.0},
			{"id": "13", "label": "Registry B", "foreign_id": "reg_b", "category": "company", "count": 7.0},
			{"id": "20", "label": "Test", "foreign_id": "test_case", "category": "casefile", "count": 3.0},
		},
		entities: func(string, string, int) Page {
			return Page{Items: []api.Object{company, ownership}, Total: 2}
		},
		entity: map[string]api.Object{"c1": company, "o1": ownership, "p1": person},
	}
}

// session drives the model like the Bubble Tea runtime does, but
// synchronously: commands run until they produce no more messages.
type session struct {
	t    *testing.T
	m    tea.Model
	quit bool
	// suspended is set by ctrl+z (the runtime would suspend the process).
	suspended bool
	clips     []string
}

func start(t *testing.T, o Options) *session {
	t.Helper()
	if o.Open == nil {
		o.Open = func(string) error { return nil }
	}
	s := &session{t: t, m: newModel(t.Context(), o)}
	s.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	s.run(s.m.Init())
	return s
}

func (s *session) send(msg tea.Msg) {
	s.t.Helper()
	var cmd tea.Cmd
	s.m, cmd = s.m.Update(msg)
	s.run(cmd)
}

func (s *session) run(cmd tea.Cmd) {
	s.t.Helper()
	for _, msg := range exec(cmd) {
		switch msg.(type) {
		case tea.QuitMsg:
			s.quit = true
		case tea.SuspendMsg:
			s.suspended = true
		default:
			if fmt.Sprintf("%T", msg) == "tea.setClipboardMsg" {
				s.clips = append(s.clips, fmt.Sprint(msg))
				continue
			}
			s.send(msg)
		}
	}
}

// exec runs a command and flattens batches. Commands that block, such as the
// cursor blink of a text input, are dropped.
func exec(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		// Batches (tea.BatchMsg) and sequences (an unexported []tea.Cmd type)
		// run their commands in order here.
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
			var out []tea.Msg
			for i := range v.Len() {
				out = append(out, exec(v.Index(i).Interface().(tea.Cmd))...)
			}
			return out
		}
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(20 * time.Millisecond):
		return nil
	}
}

func (s *session) keys(keys ...string) {
	s.t.Helper()
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "ctrl+s":
			msg = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "ctrl+z":
			msg = tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		s.send(msg)
	}
}

func (s *session) typeText(text string) {
	s.t.Helper()
	for _, r := range text {
		s.keys(string(r))
	}
}

func (s *session) screen() string { return ansi.Strip(s.m.View().Content) }

func (s *session) model() model { return s.m.(model) }

func (s *session) contains(want ...string) {
	s.t.Helper()
	screen := s.screen()
	for _, w := range want {
		if !strings.Contains(screen, w) {
			s.t.Fatalf("screen does not contain %q:\n%s", w, screen)
		}
	}
}

func (s *session) depth(want int) {
	s.t.Helper()
	if got := len(s.model().stack); got != want {
		s.t.Fatalf("stack depth = %d, want %d", got, want)
	}
}

func TestBrowseCollectionsToEntity(t *testing.T) {
	f := newFake()
	s := start(t, Options{Backend: f, Host: "https://aleph.example.org"})
	s.contains("Datasets │ Investigations │ Status", "Leak A", "Registry B", "48,210", "https://aleph.example.org", "1 of 2 datasets")

	s.keys("enter")
	s.depth(2)
	if c := f.lastCall(); c.collection != "12" || c.query != "" || c.offset != 0 {
		t.Fatalf("entities call = %+v", c)
	}
	s.contains("Datasets ❯ Leak A", "Acme Corp", "Company", "Ownership")

	s.keys("enter")
	s.depth(3)
	s.contains("Acme Corp", "jurisdiction", "pa", "line one line two", "Leak A ›")
	if strings.Contains(s.screen(), "alephUrl") {
		t.Fatal("alephUrl should be hidden")
	}

	s.keys("esc", "esc")
	s.depth(1)
	s.keys("esc")
	s.depth(1)
	s.keys("q")
	if !s.quit {
		t.Fatal("q did not quit")
	}
}

func TestBrowseNestedEntityAndText(t *testing.T) {
	f := newFake()
	s := start(t, Options{Backend: f, Collection: f.collections[0]})
	s.depth(2)
	// Ownership is the second row; its owner is a nested Person.
	s.keys("j", "enter")
	s.contains("asset", "Acme Corp [Company] ›", "owner", "Jane Doe [Person] ›")

	// Rows: id, schema, asset, owner.
	s.keys("j", "j", "j", "enter")
	s.depth(4)
	s.contains("Ownership ❯ Jane Doe")

	// id row: enter shows the value as text.
	s.keys("g", "enter")
	s.depth(5)
	s.contains("❯ id", "p1")
	s.keys("y")
	if len(s.clips) != 1 || s.clips[0] != "p1" {
		t.Fatalf("clipboard = %q", s.clips)
	}
}

func TestBrowseSearch(t *testing.T) {
	f := newFake()
	s := start(t, Options{Backend: f})
	s.keys("s")
	s.depth(2)
	s.contains("Search", "COLLECTION", "Leak A")

	s.keys("/")
	if !s.model().top().capturing() {
		t.Fatal("search input not focused")
	}
	// q and esc-like keys go to the input while it has the focus.
	s.typeText("acme q")
	if s.quit {
		t.Fatal("q in the search input quit the browser")
	}
	s.keys("enter")
	if c := f.lastCall(); c.query != "acme q" || c.collection != "" {
		t.Fatalf("entities call = %+v", c)
	}
	s.contains(`Search "acme q"`)

	// esc in the input restores the last query and keeps the view.
	s.keys("/")
	s.typeText("xyz")
	s.keys("esc")
	s.depth(2)
	s.contains(`Search "acme q"`)
}

func TestBrowseStartQuery(t *testing.T) {
	f := newFake()
	s := start(t, Options{Backend: f, Query: "jane"})
	s.depth(2)
	if c := f.lastCall(); c.query != "jane" {
		t.Fatalf("entities call = %+v", c)
	}
}

func TestBrowseLoadsMorePages(t *testing.T) {
	f := newFake()
	f.entities = func(_, _ string, offset int) Page {
		var items []api.Object
		for i := range min(50, 120-offset) {
			items = append(items, api.Object{"id": fmt.Sprint(offset + i), "schema": "Person", "caption": fmt.Sprintf("P%d", offset+i)})
		}
		return Page{Items: items, Total: 120, More: offset+50 < 120}
	}
	s := start(t, Options{Backend: f, Query: "p"})
	if c := f.lastCall(); c.offset != 0 {
		t.Fatalf("first call = %+v", c)
	}
	s.keys("G")
	if c := f.lastCall(); c.offset != 50 {
		t.Fatalf("after G call = %+v", c)
	}
	s.keys("G")
	if c := f.lastCall(); c.offset != 100 {
		t.Fatalf("after second G call = %+v", c)
	}
	s.keys("G")
	if c := f.lastCall(); c.offset != 100 {
		t.Fatalf("loaded past the end: %+v", c)
	}
	s.contains("120 of 120 entities")
}

func TestBrowseErrorsAndOpen(t *testing.T) {
	f := newFake()
	var opened []string
	s := start(t, Options{Backend: f, Collection: f.collections[0], Open: func(u string) error {
		opened = append(opened, u)
		return nil
	}})
	s.keys("o")
	if len(opened) != 1 || opened[0] != "https://aleph.example.org/entities/c1" {
		t.Fatalf("opened = %q", opened)
	}
	s.contains("Opened https://aleph.example.org/entities/c1")

	// Ownership has no web link.
	s.keys("j", "o")
	s.contains("no web link for o1")

	f.err = errors.New("boom")
	s.keys("esc", "r")
	s.contains("boom", "no datasets")
}

func TestStalePagesAreDropped(t *testing.T) {
	f := newFake()
	s := start(t, Options{Backend: f})
	l := s.model().top().(*listView)
	l.gen++ // as if a new search started after this page was requested
	s.send(pageMsg{to: l.vid, gen: l.gen - 1, page: Page{Items: []api.Object{{"id": "x", "label": "Stale"}}}})
	if strings.Contains(s.screen(), "Stale") {
		t.Fatal("stale page was shown")
	}
	// Messages for popped views are dropped.
	s.send(pageMsg{to: 999, page: Page{Items: []api.Object{{"label": "Gone"}}}})
	if strings.Contains(s.screen(), "Gone") {
		t.Fatal("message for unknown view was shown")
	}
}

func TestHelpToggle(t *testing.T) {
	s := start(t, Options{Backend: newFake()})
	s.send(tea.WindowSizeMsg{Width: 80, Height: 20})
	// The footer of the first screen fits 80 columns.
	s.contains("enter open", "tab switch", "q quit")
	if strings.Contains(s.screen(), "copy id") {
		t.Fatal("secondary keys belong in the full help")
	}
	s.keys("?")
	s.contains("½ page down", "search all", "copy id")
	s.keys("?")
	if strings.Contains(s.screen(), "½ page down") {
		t.Fatal("help still shown")
	}
}

func TestLayoutColumns(t *testing.T) {
	cols := layoutColumns([]column{{title: "A", width: 10}, {title: "B", flex: 3}, {title: "C", flex: 1}}, 100)
	// 100 - 3*2 padding - 10 fixed = 84 shared 3:1.
	if cols[0].Width != 10 || cols[1].Width != 63 || cols[2].Width != 21 {
		t.Fatalf("widths = %d %d %d", cols[0].Width, cols[1].Width, cols[2].Width)
	}
	narrow := layoutColumns([]column{{title: "A", width: 10}, {title: "B", flex: 1}}, 12)
	if narrow[1].Width != 8 {
		t.Fatalf("flex column below minimum: %d", narrow[1].Width)
	}
}

func TestFooterFitsWidth(t *testing.T) {
	s := start(t, Options{Backend: newFake()})
	for _, w := range []int{40, 80, 110} {
		s.send(tea.WindowSizeMsg{Width: w, Height: 20})
		for _, line := range strings.Split(s.model().footer(), "\n") {
			if got := ansi.StringWidth(line); got > w {
				t.Errorf("width %d: footer line is %d wide: %q", w, got, ansi.Strip(line))
			}
		}
	}
}

func statusRow(id, label string, todo, doing, failed, done float64) api.Object {
	return api.Object{
		"collection": map[string]any{"id": id, "label": label, "links": map[string]any{"ui": "https://aleph.example.org/datasets/" + id}},
		"todo":       todo, "doing": doing, "failed": failed, "finished": done, "remaining_time": "PT5H12M42S",
	}
}

func TestStatusView(t *testing.T) {
	f := newFake()
	f.status = [][]api.Object{
		{statusRow("12", "Leak A", 2, 1, 1, 6), statusRow("13", "Registry B", 10, 0, 0, 0)},
		// After the refresh Leak A is done and gone, a new collection appears.
		{statusRow("13", "Registry B", 5, 0, 0, 5), statusRow("14", "Leak C", 1, 0, 0, 1)},
	}
	s := start(t, Options{Backend: f, Status: true})
	s.depth(1)
	s.contains("Datasets │ Investigations │ Status", "Leak A", "Registry B", "70%", "5h12m", "2 collections · 35% overall", "every 3s")

	// The cursor follows the collection across refreshes.
	s.keys("j")
	v := s.model().top().(*statusView)
	s.send(statusTickMsg{to: v.vid})
	if f.statusCalls != 2 {
		t.Fatalf("status calls = %d", f.statusCalls)
	}
	s.contains("Leak C", "50%")
	if strings.Contains(s.screen(), "Leak A") {
		t.Fatal("stale row after refresh")
	}
	if c := v.selected(); str(c, "id") != "13" {
		t.Fatalf("cursor on %v, want 13", c)
	}

	s.keys("y")
	if len(s.clips) != 1 || s.clips[0] != "13" {
		t.Fatalf("clipboard = %q", s.clips)
	}
	s.keys("enter")
	s.depth(2)
	if c := f.lastCall(); c.collection != "13" {
		t.Fatalf("entities call = %+v", c)
	}
	s.contains("Status ❯ Registry B")

}

func TestStatusTab(t *testing.T) {
	f := newFake()
	s := start(t, Options{Backend: f})
	if f.statusCalls != 0 {
		t.Fatalf("status loaded before its tab was opened: %d calls", f.statusCalls)
	}
	// tab cycles through datasets, investigations and the status.
	s.keys("tab", "tab")
	s.depth(1)
	v, ok := s.model().top().(*statusView)
	if !ok {
		t.Fatalf("top = %T, want the status", s.model().top())
	}
	s.contains("no collections are being processed")
	if f.statusCalls != 1 {
		t.Fatalf("status calls = %d", f.statusCalls)
	}
	s.keys("tab")
	s.contains("1 of 2 datasets")
	// On another tab the status skips its refresh and catches up when shown.
	s.send(statusTickMsg{to: v.vid})
	if f.statusCalls != 1 {
		t.Fatalf("hidden status refreshed: %d calls", f.statusCalls)
	}
	s.keys("shift+tab")
	if s.model().top() != v || f.statusCalls != 2 {
		t.Fatalf("top = %T, status calls = %d", s.model().top(), f.statusCalls)
	}

	// Deeper in, tab belongs to the view and p is no key anymore.
	s.keys("shift+tab", "enter", "p")
	s.depth(2)
}

func TestStatusOldServerRows(t *testing.T) {
	c := statusCollection(api.Object{"collection_id": 7.0, "name": "Old"})
	if str(c, "id") != "7" || str(c, "label") != "Old" {
		t.Fatalf("collection = %v", c)
	}
}

func TestStartCollectionWithQuery(t *testing.T) {
	f := newFake()
	start(t, Options{Backend: f, Collection: f.collections[0], Query: "acme"})
	if c := f.lastCall(); c.collection != "12" || c.query != "acme" {
		t.Fatalf("entities call = %+v", c)
	}
}

func TestFitsScreen(t *testing.T) {
	f := newFake()
	f.status = [][]api.Object{{statusRow("12", "Leak A", 2, 1, 1, 6)}}
	s := start(t, Options{Backend: f, Host: "https://aleph.example.org"})
	for _, size := range [][2]int{{80, 20}, {120, 30}, {50, 12}} {
		s.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, keys := range [][]string{nil, {"tab", "tab"}, {"tab"}, {"enter"}, {"enter"}, {"?"}} {
			s.keys(keys...)
			lines := strings.Split(s.m.View().Content, "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d after %v: %d lines", size[0], size[1], keys, len(lines))
			}
			for _, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%dx%d after %v: line %d wide: %q", size[0], size[1], keys, w, ansi.Strip(l))
				}
			}
		}
		s.keys("?", "esc", "esc", "esc")
	}
}

func TestListStopsAtMaxResults(t *testing.T) {
	f := newFake()
	f.entities = func(_, _ string, offset int) Page {
		n := min(1000, MaxResults-offset)
		items := make([]api.Object, n)
		for i := range items {
			items[i] = api.Object{"id": fmt.Sprint(offset + i), "schema": "Person"}
		}
		return Page{Items: items, Total: 10000, TotalAtLeast: true, More: true}
	}
	s := start(t, Options{Backend: f, Query: "x"})
	for range 12 {
		s.keys("G")
	}
	if c := f.lastCall(); c.offset != 9000 {
		t.Fatalf("last call = %+v, want offset 9000 and no request past the limit", c)
	}
	s.contains("9999 of 10,000+ entities", "only the first 9,999 can be listed, narrow the search")
}

func TestSuspend(t *testing.T) {
	s := start(t, Options{Backend: newFake()})
	s.keys("ctrl+z")
	if !s.suspended || s.quit {
		t.Fatalf("suspended = %v, quit = %v", s.suspended, s.quit)
	}
	// ctrl+z works in a text input too.
	s.suspended = false
	s.keys("/", "ctrl+z")
	if !s.suspended {
		t.Fatal("ctrl+z in the search input did not suspend")
	}
}

func TestStatusPausesWhileCovered(t *testing.T) {
	f := newFake()
	f.status = [][]api.Object{{statusRow("12", "Leak A", 2, 1, 1, 6)}}
	s := start(t, Options{Backend: f, Status: true})
	v := s.model().top().(*statusView)
	if f.statusCalls != 1 {
		t.Fatalf("status calls = %d", f.statusCalls)
	}
	s.keys("enter") // open Leak A on top of the status
	s.depth(2)
	s.send(statusTickMsg{to: v.vid})
	s.send(statusTickMsg{to: v.vid})
	if f.statusCalls != 1 {
		t.Fatalf("covered status refreshed: %d calls", f.statusCalls)
	}
	// Back on the status: one refresh right away.
	s.keys("esc")
	if f.statusCalls != 2 {
		t.Fatalf("status calls after return = %d, want 2", f.statusCalls)
	}
	s.send(statusTickMsg{to: v.vid})
	if f.statusCalls != 3 {
		t.Fatalf("visible status did not refresh: %d calls", f.statusCalls)
	}
	// Returning without a skipped tick does not refresh twice.
	s.keys("enter", "esc")
	if f.statusCalls != 3 {
		t.Fatalf("status calls = %d, want 3", f.statusCalls)
	}
}
