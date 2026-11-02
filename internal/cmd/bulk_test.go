package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEntityStream(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/2/collections/5":
			reply(w, map[string]any{"id": "5", "label": "Leak A"})
		case "/api/2/collections/5/_stream":
			if got := r.URL.Query()["include"]; strings.Join(got, ",") != "id,schema,properties" {
				t.Errorf("include = %v", got)
			}
			_, _ = w.Write([]byte(`{"id":"a","schema":"Person","properties":{"name":["Jane"]}}` + "\n" + `{"id":"b","schema":"Company"}` + "\n"))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	h.login()
	stdout, _, err := h.run("entity", "stream", "5", "-s", "Person")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout = %q", stdout)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil || first["id"] != "a" {
		t.Fatalf("first = %v err=%v", first, err)
	}
	if props := first["properties"].(map[string]any); props["alephUrl"] == nil {
		t.Fatal("alephUrl must be added to streamed entities")
	}
}

func TestEntityStreamToFile(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_stream") {
			_, _ = w.Write([]byte(`{"id":"a"}` + "\n"))
			return
		}
		reply(w, map[string]any{"id": "5"})
	})
	h.login()
	out := filepath.Join(t.TempDir(), "out.json")
	if _, _, err := h.run("entity", "stream", "5", "-o", out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), `"id":"a"`) {
		t.Fatalf("file = %q", b)
	}
}

func TestEntityWriteMany(t *testing.T) {
	var sizes []int
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/2/collections" && r.Method == "GET":
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "5", "foreign_id": "leak_a"}}})
		case r.URL.Path == "/api/2/collections/5/_bulk":
			var chunk []map[string]any
			_ = json.NewDecoder(r.Body).Decode(&chunk)
			sizes = append(sizes, len(chunk))
			if r.URL.Query().Get("entityset_id") != "es1" {
				t.Errorf("query = %v", r.URL.Query())
			}
			reply(w, map[string]any{})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	h.login()
	h.stdin = `{"id":"1","schema":"Person"}` + "\n\n" + `{"id":"2","schema":"Person"}` + "\n" + `{"id":"3","schema":"Person"}`
	_, stderr, err := h.run("entity", "write-many", "leak_a", "-c", "2", "-e", "es1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 2 || sizes[0] != 2 || sizes[1] != 1 {
		t.Fatalf("sizes = %v", sizes)
	}
	if !strings.Contains(stderr, "[leak_a] Bulk load entities: 3...") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestEntityWriteManyInvalidJSON(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "5", "foreign_id": "leak_a"}}})
	})
	h.login()
	h.stdin = "{not json}\n"
	_, _, err := h.run("entity", "write-many", "leak_a")
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestEntityWriteSingle(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/2/collections":
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "5", "foreign_id": "leak_a"}}})
		case r.URL.Path == "/api/2/entities" && r.Method == "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["collection_id"] != "5" || body["schema"] != "Person" {
				t.Errorf("body = %v", body)
			}
			reply(w, map[string]any{"id": "x"})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	h.login()
	h.stdin = `{"schema":"Person","properties":{"name":["Jane"]}}` + "\n"
	if _, _, err := h.run("entity", "write", "leak_a"); err != nil {
		t.Fatal(err)
	}
	h.stdin = "\n"
	if _, _, err := h.run("entity", "write", "leak_a"); err == nil {
		t.Fatal("empty input must fail")
	}
}

func TestEntityMap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.csv"), []byte("name,city\nAnna,Leipzig\nBob,Leipzig\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := "dataset: demo\nqueries:\n  - csv_url: p.csv\n    entities:\n" +
		"      city:\n        schema: Address\n        keys: [city]\n        properties:\n          city: {column: city}\n" +
		"      person:\n        schema: Person\n        keys: [name]\n        properties:\n          name: {column: name}\n          addressEntity: {entity: city}\n"
	mf := filepath.Join(dir, "m.yml")
	if err := os.WriteFile(mf, []byte(m), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(http.ResponseWriter, *http.Request) { t.Error("map must not call the API") })
	stdout, _, err := h.run("entity", "map", mf)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 { // one shared address, two people
		t.Fatalf("stdout = %q", stdout)
	}
	var e map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil || e["id"] == "" || e["schema"] == "" {
		t.Fatalf("first = %v err=%v", e, err)
	}
}
