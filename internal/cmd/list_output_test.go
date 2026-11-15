package cmd

import (
	"net/http"
	"strings"
	"testing"
)

func emptyListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/2/collections":
		if r.URL.Query().Get("filter:foreign_id") != "" {
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "5", "foreign_id": "leak_a"}}})
			return
		}
		reply(w, map[string]any{"total": 0, "results": []any{}})
	default:
		reply(w, map[string]any{"total": 0, "results": []any{}})
	}
}

func TestEmptyListsPrintEmptyJSONArray(t *testing.T) {
	h := newHarness(t, emptyListHandler)
	h.login()
	for _, args := range [][]string{
		{"collection", "list", "--json", "id"},
		{"collection", "xref", "list", "leak_a", "--json", "all"},
		{"entityset", "list", "--json", "id"},
		{"entityset", "items", "es1", "--json", "all"},
		{"search", "acme", "--json", "id"},
	} {
		out, _, err := h.run(args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.TrimSpace(out) != "[]" {
			t.Errorf("%v: output = %q, want []", args, out)
		}
	}
}

func TestEmptyEntitySetListsSayNothingFound(t *testing.T) {
	h := newHarness(t, emptyListHandler)
	h.login()
	for args, want := range map[string]string{
		"entityset list":      "No entity sets found",
		"entityset items es1": "No items found",
	} {
		out, stderr, err := h.run(strings.Fields(args)...)
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		if out != "" || !strings.Contains(stderr, want) {
			t.Errorf("%s: stdout = %q, stderr = %q", args, out, stderr)
		}
	}
}
