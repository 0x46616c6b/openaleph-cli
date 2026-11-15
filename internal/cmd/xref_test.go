package cmd

import (
	"net/http"
	"strings"
	"testing"
)

func xrefHandler(t *testing.T, seen *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/2/collections":
			fid := r.URL.Query().Get("filter:foreign_id")
			id := map[string]string{"leak_a": "5", "leak_b": "8"}[fid]
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": id, "foreign_id": fid, "label": "Leak " + fid}}})
			return
		case "/api/2/collections/5":
			reply(w, map[string]any{"id": "5", "foreign_id": "leak_a", "label": "Leak A"})
			return
		}
		*seen = append(*seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.URL.Path == "/api/2/collections/5/xref" && r.Method == "GET":
			reply(w, map[string]any{"total": 3, "results": []any{
				map[string]any{
					"score": 0.912, "judgement": "no_judgement",
					"entity": map[string]any{"id": "e1", "schema": "Person", "caption": "Jane Doe"},
					"match": map[string]any{"id": "m1", "schema": "Person", "caption": "Jane A. Doe",
						"collection": map[string]any{"id": "8", "label": "Leak B"}},
				},
				map[string]any{
					"score": 0.7, "judgement": "positive",
					"entity": map[string]any{"id": "e2", "schema": "Company", "caption": "Acme"},
					"match": map[string]any{"id": "m2", "schema": "Company", "caption": "Acme Ltd",
						"collection": map[string]any{"id": "8", "label": "Leak B"}},
				},
			}})
		case r.Method == "POST":
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestXrefRun(t *testing.T) {
	var seen []string
	h := newHarness(t, xrefHandler(t, &seen))
	h.login()
	_, stderr, err := h.run("collection", "xref", "run", "5")
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "POST /api/2/collections/5/xref?" {
		t.Fatalf("seen = %v", seen)
	}
	if !strings.Contains(stderr, "Started cross-referencing of Leak A") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestXrefListTable(t *testing.T) {
	var seen []string
	h := newHarness(t, xrefHandler(t, &seen))
	h.login()
	stdout, stderr, err := h.run("collection", "xref", "list", "leak_a", "--match", "leak_b", "--min-score", "0.8", "--limit", "2")
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("seen = %v", seen)
	}
	for _, want := range []string{"filter%3Amatch_collection_id=8", "filter%3Agte%3Ascore=0.8", "limit=2"} {
		if !strings.Contains(seen[0], want) {
			t.Errorf("missing %q in %s", want, seen[0])
		}
	}
	for _, want := range []string{"SCORE", "MATCH COLLECTION", "0.91", "Jane Doe", "Jane A. Doe", "Leak B", "positive"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "Showing 2 of 3 matches") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestXrefListJQ(t *testing.T) {
	var seen []string
	h := newHarness(t, xrefHandler(t, &seen))
	h.login()
	stdout, _, err := h.run("collection", "xref", "list", "5", "--jq", ".[].match.id")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "m1\nm2\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestXrefExport(t *testing.T) {
	var seen []string
	h := newHarness(t, xrefHandler(t, &seen))
	h.login()
	_, stderr, err := h.run("collection", "xref", "export", "leak_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "POST /api/2/collections/5/xref.xlsx?" {
		t.Fatalf("seen = %v", seen)
	}
	if !strings.Contains(stderr, "oa export list") {
		t.Fatalf("stderr = %q", stderr)
	}
}
