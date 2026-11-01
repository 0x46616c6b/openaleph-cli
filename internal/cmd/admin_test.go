package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// collectionHandler serves collection 5 ("leak_a") and records mutating requests.
func collectionHandler(t *testing.T, seen *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			*seen = append(*seen, r.Method+" "+r.URL.RequestURI())
		}
		switch {
		case r.URL.Path == "/api/2/collections" && r.Method == "GET":
			reply(w, map[string]any{"total": 1, "results": []any{
				map[string]any{"id": "5", "foreign_id": "leak_a", "label": "Leak A", "category": "leak", "languages": []any{"de"}},
			}})
		case r.URL.Path == "/api/2/collections/5" && r.Method == "GET":
			reply(w, map[string]any{"id": "5", "foreign_id": "leak_a", "label": "Leak A", "category": "leak"})
		case r.URL.Path == "/api/2/collections" && r.Method == "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["foreign_id"] != "new_one" || body["label"] != "New One" || body["category"] != "news" {
				t.Errorf("create body = %v", body)
			}
			reply(w, map[string]any{"id": "9"})
		case r.URL.Path == "/api/2/collections/5" && r.Method == "PUT":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["label"] != "Renamed" || body["category"] != "leak" || body["foreign_id"] != "leak_a" {
				t.Errorf("edit body must merge changes into existing data: %v", body)
			}
			reply(w, map[string]any{"id": "5"})
		default:
			reply(w, map[string]any{"status": "ok"})
		}
	}
}

func TestDestructiveCommandsNeedConfirmation(t *testing.T) {
	var seen []string
	h := newHarness(t, collectionHandler(t, &seen))
	h.login()
	for _, args := range [][]string{
		{"collection", "delete", "leak_a"},
		{"collection", "flush", "leak_a"},
		{"entity", "delete", "e1"},
		{"entityset", "delete", "es1"},
	} {
		_, _, err := h.run(args...)
		if err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
	if len(seen) != 0 {
		t.Fatalf("nothing may be sent without confirmation: %v", seen)
	}
}

func TestCollectionDeleteAndFlush(t *testing.T) {
	var seen []string
	h := newHarness(t, collectionHandler(t, &seen))
	h.login()
	if _, _, err := h.run("collection", "delete", "leak_a", "--yes", "--sync"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run("collection", "flush", "5", "--yes"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DELETE /api/2/collections/5?sync=true",
		"DELETE /api/2/collections/5?keep_metadata=true&sync=false",
	}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("seen = %v", seen)
	}
}

func TestCollectionCreate(t *testing.T) {
	var seen []string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			reply(w, map[string]any{"total": 0, "results": []any{}})
			return
		}
		collectionHandler(t, &seen)(w, r)
	})
	h.login()
	stdout, _, err := h.run("collection", "create", "New One", "--foreign-id", "new_one", "--category", "news")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "9" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestCollectionEditMergesExisting(t *testing.T) {
	var seen []string
	h := newHarness(t, collectionHandler(t, &seen))
	h.login()
	if _, _, err := h.run("collection", "edit", "leak_a", "--label", "Renamed"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run("collection", "edit", "leak_a"); err == nil {
		t.Fatal("edit without flags must fail")
	}
}

func TestReindexAndReingest(t *testing.T) {
	var seen []string
	h := newHarness(t, collectionHandler(t, &seen))
	h.login()
	if _, _, err := h.run("collection", "reindex", "leak_a", "--flush"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run("collection", "reingest", "leak_a", "--index"); err != nil {
		t.Fatal(err)
	}
	want := "POST /api/2/collections/5/reindex?flush=true&sync=false|POST /api/2/collections/5/reingest?index=true"
	if strings.Join(seen, "|") != want {
		t.Fatalf("seen = %v", seen)
	}
}

func TestEntitySetCreateAndDelete(t *testing.T) {
	var seen []string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/2/collections":
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "5", "foreign_id": "leak_a"}}})
		case r.URL.Path == "/api/2/entitysets" && r.Method == "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["type"] != "list" || body["label"] != "Suspects" || body["collection_id"] != "5" {
				t.Errorf("body = %v", body)
			}
			reply(w, map[string]any{"id": "es7"})
		default:
			seen = append(seen, r.Method+" "+r.URL.Path)
			reply(w, map[string]any{})
		}
	})
	h.login()
	stdout, _, err := h.run("entityset", "create", "leak_a", "Suspects")
	if err != nil || strings.TrimSpace(stdout) != "es7" {
		t.Fatalf("stdout=%q err=%v", stdout, err)
	}
	if _, _, err := h.run("entityset", "delete", "es7", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run("entity", "delete", "e1", "--yes"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, "|") != "DELETE /api/2/entitysets/es7|DELETE /api/2/entities/e1" {
		t.Fatalf("seen = %v", seen)
	}
}
