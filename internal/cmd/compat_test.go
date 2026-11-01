package cmd

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// compatServer serves foreign ID "leak_a" as collection 5 and records mutations.
func compatServer(t *testing.T, seen *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/2/collections" && r.Method == "GET":
			if r.URL.Query().Get("filter:foreign_id") == "leak_a" {
				reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "5", "foreign_id": "leak_a"}}})
				return
			}
			reply(w, map[string]any{"total": 0, "results": []any{}})
		case r.URL.Path == "/api/2/entitysets" && r.Method == "POST":
			reply(w, map[string]any{"id": "es9"})
		default:
			*seen = append(*seen, r.Method+" "+r.URL.RequestURI())
			reply(w, map[string]any{"id": "ok"})
		}
	}
}

func TestCompatCommandsAreHidden(t *testing.T) {
	h := newHarness(t, func(http.ResponseWriter, *http.Request) {})
	stdout, _, err := h.run("--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"crawldir", "write-entities", "stream-entities", "make-list"} {
		if strings.Contains(stdout, name) {
			t.Errorf("%s must be hidden from --help", name)
		}
	}
}

func TestCompatDeleteFlushReindexReingest(t *testing.T) {
	var seen []string
	h := newHarness(t, compatServer(t, &seen))
	h.login()
	for _, args := range [][]string{
		{"delete", "-f", "leak_a", "--sync", "--yes"},
		{"flush", "-f", "leak_a", "-y"},
		{"reindex", "-f", "leak_a", "--flush"},
		{"reingest", "-f", "leak_a", "--index"},
		{"delete-entity", "e1", "--yes"},
	} {
		if _, _, err := h.run(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	want := strings.Join([]string{
		"DELETE /api/2/collections/5?sync=true",
		"DELETE /api/2/collections/5?keep_metadata=true&sync=false",
		"POST /api/2/collections/5/reindex?flush=true&sync=false",
		"POST /api/2/collections/5/reingest?index=true",
		"DELETE /api/2/entities/e1",
	}, "|")
	if got := strings.Join(seen, "|"); got != want {
		t.Fatalf("seen:\n%s\nwant:\n%s", got, want)
	}
}

func TestCompatUnknownCollection(t *testing.T) {
	var seen []string
	h := newHarness(t, compatServer(t, &seen))
	h.login()
	_, _, err := h.run("delete", "-f", "nope")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("nothing may be sent: %v", seen)
	}
}

func TestCompatRequiresForeignID(t *testing.T) {
	h := newHarness(t, func(http.ResponseWriter, *http.Request) {})
	h.login()
	if _, _, err := h.run("delete"); err == nil || !strings.Contains(err.Error(), "foreign-id") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompatWriteEntities(t *testing.T) {
	var seen []string
	h := newHarness(t, compatServer(t, &seen))
	h.login()
	h.stdin = `{"id":"1"}` + "\n" + `{"id":"2"}` + "\n"
	if _, _, err := h.run("write-entities", "-f", "leak_a", "-c", "10", "--unsafe", "-e", "es1"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || !strings.HasPrefix(seen[0], "POST /api/2/collections/5/_bulk?") ||
		!strings.Contains(seen[0], "entityset_id=es1") || !strings.Contains(seen[0], "safe=false") {
		t.Fatalf("seen = %v", seen)
	}
}

func TestCompatMakeListNoTrailingNewline(t *testing.T) {
	var seen []string
	h := newHarness(t, compatServer(t, &seen))
	h.login()
	stdout, _, err := h.run("make-list", "-f", "leak_a", "Suspects")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "es9" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestCompatCrawldirUsesForeignID(t *testing.T) {
	var seen []string
	h := newHarness(t, compatServer(t, &seen))
	h.login()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := h.run("crawldir", "-f", "leak_a", "-p", "2", "-i", dir); err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if len(seen) != 1 || !strings.Contains(seen[0], "/api/2/collections/5/ingest") || !strings.Contains(seen[0], "index=false") {
		t.Fatalf("seen = %v", seen)
	}
}

func TestCompatDestructiveCommandsNeedConfirmation(t *testing.T) {
	var seen []string
	h := newHarness(t, compatServer(t, &seen))
	h.login()
	for _, args := range [][]string{
		{"delete", "-f", "leak_a"},
		{"flush", "-f", "leak_a"},
		{"delete-entity", "e1"},
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
