package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

func startSchemas(t *testing.T) (*session, *fakeBackend) {
	t.Helper()
	f := newFake()
	f.schemaCounts = []api.FacetValue{
		{ID: "Person", Label: "People", Count: 1200},
		{ID: "Address", Label: "Addresses", Count: 300},
		{ID: "Ownership", Label: "Ownership", Count: 40},
	}
	s := start(t, Options{Backend: f, Collection: f.collections[0]})
	return s, f
}

func TestSchemaFilterSingle(t *testing.T) {
	s, f := startSchemas(t)
	s.keys("f")
	s.depth(3)
	s.contains("Filter Leak A by schema", "All schemas", "Person", "People", "1,200", "Address", "Ownership")
	if q := f.facetQueries[0]; q.CollectionID != "12" || q.Schemas != nil {
		t.Fatalf("facet query = %+v", q)
	}

	// Typing narrows the list and moves to the first match.
	s.typeText("addr")
	if strings.Contains(s.screen(), "Ownership") {
		t.Fatal("not narrowed")
	}
	s.keys("enter")
	s.depth(2)
	if c := f.lastCall(); c.collection != "12" || !slices.Equal(c.schemas, []string{"Address"}) || c.offset != 0 {
		t.Fatalf("entities call = %+v", c)
	}
	s.contains("Leak A [Address]", "schema: Address (F clears)")

	// F clears the filter.
	s.keys("F")
	if c := f.lastCall(); c.schemas != nil {
		t.Fatalf("after F: %+v", c)
	}
	if strings.Contains(s.screen(), "[Address]") {
		t.Fatal("filter still shown")
	}
}

func TestSchemaFilterMultiAndClear(t *testing.T) {
	s, f := startSchemas(t)
	// Select Person and Ownership with space, then apply.
	s.keys("f", "down", "space", "down", "down", "space", "enter")
	if c := f.lastCall(); !slices.Equal(c.schemas, []string{"Ownership", "Person"}) {
		t.Fatalf("entities call = %+v", c)
	}
	s.contains("[Ownership, Person]")

	// The picker shows the active filter; "All schemas" clears it.
	s.keys("f")
	s.contains("[x] Ownership", "[x] Person", "[ ] Address")
	s.keys("space", "enter")
	if c := f.lastCall(); c.schemas != nil {
		t.Fatalf("after all: %+v", c)
	}

	// esc closes the picker without changes.
	calls := len(f.calls)
	s.keys("f", "down", "esc")
	s.depth(2)
	if len(f.calls) != calls {
		t.Fatal("esc applied a filter")
	}
}

func TestSchemaFilterKeepsQuery(t *testing.T) {
	s, f := startSchemas(t)
	s.keys("/")
	s.typeText("acme")
	s.keys("enter", "f")
	if q := f.facetQueries[0]; q.Query != "acme" {
		t.Fatalf("facet query = %+v", q)
	}
	s.keys("down", "enter")
	if c := f.lastCall(); c.query != "acme" || !slices.Equal(c.schemas, []string{"Person"}) {
		t.Fatalf("entities call = %+v", c)
	}
	s.contains(`Leak A "acme" [Person]`)
}

func TestStartWithSchemas(t *testing.T) {
	f := newFake()
	start(t, Options{Backend: f, Schemas: []string{"Person"}})
	if c := f.lastCall(); c.collection != "" || !slices.Equal(c.schemas, []string{"Person"}) {
		t.Fatalf("entities call = %+v", c)
	}
}

func TestSchemaPickerFitsScreen(t *testing.T) {
	s, f := startSchemas(t)
	for i := range 40 {
		f.schemaCounts = append(f.schemaCounts, api.FacetValue{ID: fmt.Sprintf("Schema%d", i), Count: i})
	}
	for _, size := range [][2]int{{80, 24}, {50, 12}} {
		s.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		s.keys("f")
		for range 30 {
			s.keys("down")
		}
		lines := strings.Split(s.m.View().Content, "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: line %d wide: %q", size[0], size[1], w, ansi.Strip(l))
			}
		}
		s.contains("❯ [ ] Schema26") // the cursor row (after Person, Address, Ownership) scrolled into view
		s.keys("esc")
	}
}
