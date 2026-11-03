package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

type adminCall struct {
	op, id string
	data   api.Object
	flag   bool
}

// fakeAdmin is a fakeBackend that can also change collections.
type fakeAdmin struct {
	*fakeBackend
	calls []adminCall
	fail  error
}

func (f *fakeAdmin) CreateCollection(_ context.Context, data api.Object) (api.Object, error) {
	f.calls = append(f.calls, adminCall{op: "create", data: data})
	if f.fail != nil {
		return nil, f.fail
	}
	out := api.Object{"id": "99"}
	for k, v := range data {
		out[k] = v
	}
	return out, nil
}

func (f *fakeAdmin) UpdateCollection(_ context.Context, c, changes api.Object) (api.Object, error) {
	f.calls = append(f.calls, adminCall{op: "update", id: str(c, "id"), data: changes})
	out := api.Object{}
	for k, v := range c {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out, f.fail
}

func (f *fakeAdmin) DeleteCollection(_ context.Context, id string) error {
	f.calls = append(f.calls, adminCall{op: "delete", id: id})
	return f.fail
}

func (f *fakeAdmin) ReindexCollection(_ context.Context, id string, flush bool) error {
	f.calls = append(f.calls, adminCall{op: "reindex", id: id, flag: flush})
	return f.fail
}

func (f *fakeAdmin) ReingestCollection(_ context.Context, id string, index bool) error {
	f.calls = append(f.calls, adminCall{op: "reingest", id: id, flag: index})
	return f.fail
}

func (f *fakeAdmin) last(t *testing.T) adminCall {
	t.Helper()
	if len(f.calls) == 0 {
		t.Fatal("no admin call")
	}
	return f.calls[len(f.calls)-1]
}

func startAdmin(t *testing.T) (*session, *fakeAdmin) {
	t.Helper()
	f := &fakeAdmin{fakeBackend: newFake()}
	f.collections[0]["languages"] = []any{"en"}
	return start(t, Options{Backend: f}), f
}

func TestCreateCollection(t *testing.T) {
	s, f := startAdmin(t)
	s.keys("n")
	s.depth(2)
	s.contains("New dataset", "Label", "Foreign ID", "Category")
	if strings.Contains(s.screen(), "Casefile") {
		t.Fatal("casefile is the investigations list, not a form field")
	}

	// A label is required.
	s.keys("ctrl+s")
	s.contains("Label is required")
	if len(f.calls) != 0 {
		t.Fatal("saved without a label")
	}

	// q is text in a form, not quit.
	s.typeText("Leak q")
	if s.quit {
		t.Fatal("q in the form quit the browser")
	}
	// Foreign ID empty, category: one to the right of "other".
	s.keys("tab", "tab", "right", "tab", "tab")
	s.typeText("de, EN")
	s.keys("enter")

	c := f.last(t)
	if c.op != "create" || c.data["label"] != "Leak q" || c.data["foreign_id"] != "Leak q" ||
		c.data["category"] != "news" || c.data["casefile"] != nil {
		t.Fatalf("create = %+v", c)
	}
	if langs := c.data["languages"].([]string); strings.Join(langs, ",") != "de,en" {
		t.Fatalf("languages = %v", langs)
	}
	s.depth(1)
	s.contains("Created Leak q", "3 datasets")
	if l := s.model().top().(*listView); str(l.selected(), "id") != "99" {
		t.Fatalf("selected %v, want the new collection", l.selected())
	}
}

func TestCreateInvestigation(t *testing.T) {
	s, f := startAdmin(t)
	s.contains("Leak A")
	if strings.Contains(s.screen(), "Test") {
		t.Fatal("investigation in the datasets list")
	}
	s.keys("tab")
	s.contains("Test", "test_case", "1 of 1 investigations")
	if strings.Contains(s.screen(), "Leak A") || strings.Contains(s.screen(), "CATEGORY") {
		t.Fatal("datasets or category column in the investigations list")
	}

	s.keys("n")
	s.contains("New investigation", "Label")
	if strings.Contains(s.screen(), "Category") {
		t.Fatal("investigations have a fixed category")
	}
	s.typeText("Case 2")
	s.keys("ctrl+s")
	if c := f.last(t); c.op != "create" || c.data["category"] != "casefile" || c.data["label"] != "Case 2" {
		t.Fatalf("create = %+v", c)
	}
	s.contains("Created Case 2", "1 of 2 investigations")

	// Edit keeps the category; the datasets list keeps its state.
	s.keys("e", "ctrl+s")
	if c := f.last(t); c.op != "update" || c.data["category"] != "casefile" {
		t.Fatalf("update = %+v", c)
	}
	s.keys("shift+tab")
	s.contains("Leak A", "1 of 2 datasets")
}

func TestStartInInvestigation(t *testing.T) {
	f := newFake()
	s := start(t, Options{Backend: f, Collection: f.collections[2]})
	s.contains("Investigations ❯ Test")
	s.keys("esc")
	s.contains("Test", "investigations")
}

func TestCreateCollectionErrorKeepsForm(t *testing.T) {
	s, f := startAdmin(t)
	f.fail = errors.New("a collection with foreign ID \"x\" already exists")
	s.keys("n")
	s.typeText("x")
	s.keys("ctrl+s")
	s.depth(2)
	s.contains("already exists")
	f.fail = nil
	s.keys("ctrl+s")
	s.depth(1)
}

func TestEditCollection(t *testing.T) {
	s, f := startAdmin(t)
	s.keys("e")
	s.contains("Edit Leak A", "leak_a", "leak", "en")
	// Category leak -> land.
	s.keys("tab", "tab", "right", "ctrl+s")
	c := f.last(t)
	if c.op != "update" || c.id != "12" || c.data["category"] != "land" || c.data["label"] != "Leak A" ||
		c.data["foreign_id"] != "leak_a" {
		t.Fatalf("update = %+v", c)
	}
	s.depth(1)
	s.contains("Saved Leak A", "land")

	// esc cancels without saving.
	s.keys("e", "esc")
	s.depth(1)
	if len(f.calls) != 1 {
		t.Fatalf("calls = %+v", f.calls)
	}
}

func TestDeleteCollection(t *testing.T) {
	s, f := startAdmin(t)
	s.keys("D")
	s.contains("Delete Leak A", "cannot be undone", "Type leak_a to confirm")
	s.typeText("leak")
	s.keys("enter")
	s.contains(`type "leak_a" to confirm`)
	if len(f.calls) != 0 {
		t.Fatal("deleted with a wrong confirmation")
	}
	s.typeText("_a")
	s.keys("enter")
	if c := f.last(t); c.op != "delete" || c.id != "12" {
		t.Fatalf("delete = %+v", c)
	}
	s.depth(1)
	s.contains("Deleted Leak A", "Registry B", "1 of 1 datasets")
	if strings.Contains(s.screen(), "leak_a") {
		t.Fatal("deleted collection still listed")
	}
}

func TestReindexAndReingest(t *testing.T) {
	s, f := startAdmin(t)
	s.keys("i")
	s.contains("Re-index Leak A", "[ ] Flush the index first")
	s.keys("space")
	s.contains("[x] Flush the index first")
	s.keys("y")
	if c := f.last(t); c.op != "reindex" || c.id != "12" || !c.flag {
		t.Fatalf("reindex = %+v", c)
	}
	s.depth(1)
	s.contains("Re-index of Leak A started")

	s.keys("j", "I", "n")
	s.depth(1)
	if len(f.calls) != 1 {
		t.Fatal("n did not cancel")
	}
	s.keys("I", "enter")
	if c := f.last(t); c.op != "reingest" || c.id != "13" || c.flag {
		t.Fatalf("reingest = %+v", c)
	}

	// Errors stay in the dialog.
	f.fail = errors.New("403 Forbidden")
	s.keys("i", "y")
	s.depth(2)
	s.contains("403 Forbidden")
}

func TestActionsMenu(t *testing.T) {
	s, f := startAdmin(t)
	s.keys("a")
	s.depth(2)
	s.contains("Leak A", "n  new dataset", "e  edit", "D  delete", "i  re-index", "I  re-ingest")
	s.keys("esc")
	s.depth(1)

	// A key in the menu runs its action in place of the menu.
	s.keys("a", "i")
	s.depth(2)
	s.contains("Datasets ❯ Re-index Leak A")
	s.keys("esc")

	// enter runs the selected item: new collection is first.
	s.keys("a", "enter")
	s.contains("Datasets ❯ New dataset")
	if len(f.calls) != 0 {
		t.Fatal("unexpected call")
	}
}

func TestReadOnlyBackendHasNoActions(t *testing.T) {
	s := start(t, Options{Backend: newFake()})
	s.keys("a", "n", "D")
	s.depth(1)
	if strings.Contains(s.screen(), "a actions") {
		t.Fatal("actions shown for a read-only backend")
	}
}

func TestDialogsFitScreen(t *testing.T) {
	s, _ := startAdmin(t)
	for _, size := range [][2]int{{80, 24}, {50, 12}} {
		s.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, k := range []string{"n", "e", "D", "i", "a"} {
			s.keys(k)
			lines := strings.Split(s.m.View().Content, "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d %s: %d lines", size[0], size[1], k, len(lines))
			}
			for _, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%dx%d %s: line %d wide: %q", size[0], size[1], k, w, ansi.Strip(l))
				}
			}
			s.keys("esc")
			s.depth(1)
		}
	}
}

func TestReadOnlyCollection(t *testing.T) {
	s, f := startAdmin(t)
	f.collections[0]["writeable"] = false
	for _, k := range []string{"e", "D", "i", "I"} {
		s.keys(k)
		s.depth(1)
		s.contains("you cannot change Leak A: no write access")
	}
	// The menu offers only what is allowed.
	s.keys("a")
	s.contains("Leak A (read-only)", "new dataset")
	if strings.Contains(s.screen(), "delete") || strings.Contains(s.screen(), "re-index") {
		t.Fatal("write actions in the menu of a read-only collection")
	}
	s.keys("esc", "j", "e")
	s.contains("Edit Registry B") // no flag: allowed
	if len(f.calls) != 0 {
		t.Fatalf("calls = %+v", f.calls)
	}
}
