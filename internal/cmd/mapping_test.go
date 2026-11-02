package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mappingYAML = `dataset: people
queries:
  - csv_url: people.csv
    entities:
      person:
        schema: Person
        keys: [id]
        properties:
          name:
            column: name
`

func writeMapping(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func mappingHandler(t *testing.T, seen *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/2/collections" {
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "5", "foreign_id": "leak_a"}}})
			return
		}
		if r.URL.Path == "/api/2/collections/5" {
			reply(w, map[string]any{"id": "5", "foreign_id": "leak_a"})
			return
		}
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.URL.Path == "/api/2/collections/5/mappings" && r.Method == "POST":
			q, _ := body["mapping_query"].(map[string]any)
			if body["table_id"] != "t1" || q["person"] == nil || len(q) != 1 {
				t.Errorf("create body = %v", body)
			}
			reply(w, map[string]any{"id": "12"})
		case r.URL.Path == "/api/2/collections/5/mappings" && r.Method == "GET":
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "12", "table_id": "t1", "last_run_status": "success"}}})
		case r.URL.Path == "/api/2/collections/5/mappings/12" && r.Method == "GET":
			reply(w, map[string]any{"id": "12", "table_id": "t1", "query": map[string]any{"person": map[string]any{"schema": "Person"}}})
		case r.URL.Path == "/api/2/collections/5/mappings/12" && r.Method == "POST":
			reply(w, map[string]any{"id": "12", "table_id": body["table_id"], "got": body})
		default:
			reply(w, map[string]any{})
		}
	}
}

func TestMappingCreateListView(t *testing.T) {
	var seen []string
	h := newHarness(t, mappingHandler(t, &seen))
	h.login()
	stdout, _, err := h.run("mapping", "create", "leak_a", writeMapping(t, mappingYAML), "--table", "t1")
	if err != nil || strings.TrimSpace(stdout) != "12" {
		t.Fatalf("stdout=%q err=%v", stdout, err)
	}
	stdout, _, err = h.run("mapping", "list", "leak_a")
	if err != nil || !strings.Contains(stdout, "success") {
		t.Fatalf("stdout=%q err=%v", stdout, err)
	}
	stdout, _, err = h.run("mapping", "view", "leak_a", "12")
	if err != nil || !strings.Contains(stdout, "schema: Person") {
		t.Fatalf("stdout=%q err=%v", stdout, err)
	}
}

func TestMappingCreateNeedsOneQueryAndTable(t *testing.T) {
	var seen []string
	h := newHarness(t, mappingHandler(t, &seen))
	h.login()
	two := mappingYAML + "  - csv_url: b.csv\n    entities: {}\n"
	if _, _, err := h.run("mapping", "create", "leak_a", writeMapping(t, two), "--table", "t1"); err == nil || !strings.Contains(err.Error(), "exactly one query") {
		t.Errorf("err = %v", err)
	}
	if _, _, err := h.run("mapping", "create", "leak_a", writeMapping(t, mappingYAML)); err == nil {
		t.Error("--table must be required")
	}
	if len(seen) != 0 {
		t.Fatalf("seen = %v", seen)
	}
}

func TestMappingUpdateKeepsUnchangedFields(t *testing.T) {
	var seen []string
	h := newHarness(t, mappingHandler(t, &seen))
	h.login()
	if _, _, err := h.run("mapping", "update", "leak_a", "12", "--table", "t2", "--json", "all"); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := h.run("mapping", "update", "leak_a", "12", writeMapping(t, mappingYAML), "--json", "all")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"table_id":"t1"`) && !strings.Contains(stdout, `"table_id": "t1"`) {
		t.Errorf("table must be kept: %s", stdout)
	}
	if _, _, err := h.run("mapping", "update", "leak_a", "12"); err == nil {
		t.Error("update without changes must fail")
	}
}

func TestMappingTriggerDeleteFlush(t *testing.T) {
	var seen []string
	h := newHarness(t, mappingHandler(t, &seen))
	h.login()
	for _, args := range [][]string{{"mapping", "delete", "leak_a", "12"}, {"mapping", "flush", "leak_a", "12"}} {
		if _, _, err := h.run(args...); err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
	if len(seen) != 0 {
		t.Fatalf("nothing may be sent without confirmation: %v", seen)
	}
	for _, args := range [][]string{
		{"mapping", "trigger", "leak_a", "12"},
		{"mapping", "flush", "leak_a", "12", "--yes"},
		{"mapping", "delete", "5", "12", "--yes"},
	} {
		if _, _, err := h.run(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	want := "POST /api/2/collections/5/mappings/12/trigger|POST /api/2/collections/5/mappings/12/flush|DELETE /api/2/collections/5/mappings/12"
	if strings.Join(seen, "|") != want {
		t.Fatalf("seen = %v", seen)
	}
}
