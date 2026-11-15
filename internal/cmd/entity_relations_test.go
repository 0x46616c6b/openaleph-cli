package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestEntitySimilarTableAndPaging(t *testing.T) {
	var calls int
	var h *harness
	h = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/2/entities/e1/similar" {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("offset") == "" {
			if q.Get("filter:schema") != "Person" || q.Get("limit") != "2" || q.Has("filter:schemata") {
				t.Errorf("query = %v", q)
			}
			reply(w, map[string]any{"total": 5, "next": h.srv.URL + "/api/2/entities/e1/similar?offset=1&limit=1", "results": []any{
				map[string]any{"score": 0.912, "judgement": "positive", "entity": map[string]any{
					"id": "e2", "schema": "Person", "caption": "Jane Doe", "collection": map[string]any{"label": "Leak A"},
				}},
			}})
			return
		}
		reply(w, map[string]any{"total": 5, "results": []any{
			map[string]any{"score": 0.5, "entity": map[string]any{
				"id": "e3", "schema": "Person", "properties": map[string]any{"name": []any{"J. Doe"}},
			}},
		}})
	})
	h.login()
	stdout, stderr, err := h.run("entity", "similar", "e1", "--schema", "Person", "--limit", "2")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SCORE", "Jane Doe", "Leak A", "0.91", "positive", "J. Doe", "0.50"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if calls != 2 || !strings.Contains(stderr, "Showing 2 of 5") {
		t.Errorf("calls = %d, stderr = %q", calls, stderr)
	}
}

func TestEntityMentionsQueryAndJQ(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/2/entities/e1/mentions" || q.Get("q") != "invoice" || q.Get("filter:schemata") != "Email" {
			t.Errorf("request = %s %v", r.URL.Path, q)
		}
		reply(w, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": "d1", "schema": "Email", "properties": map[string]any{"subject": []any{"Re: invoice"}}},
		}})
	})
	h.login()
	stdout, _, err := h.run("entity", "mentions", "e1", "--query", "invoice", "--schemata", "Email", "--jq", ".[].id")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "d1" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestEntityNearbyEmpty(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/2/entities/a1/nearby" || r.URL.Query().Has("q") {
			t.Errorf("request = %s %v", r.URL.Path, r.URL.Query())
		}
		reply(w, map[string]any{"total": 0, "results": []any{}})
	})
	h.login()
	stdout, stderr, err := h.run("entity", "nearby", "a1")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" || !strings.Contains(stderr, "No results") {
		t.Fatalf("stdout = %q, stderr = %q", stdout, stderr)
	}
	if _, _, err := h.run("entity", "nearby", "a1", "--query", "x"); err == nil {
		t.Fatal("nearby must not accept --query")
	}
}

func TestEntityExpand(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/2/entities/e1/expand" || q.Get("limit") != "50" ||
			strings.Join(q["filter:property"], ",") != "directorshipDirector,ownershipOwner" {
			t.Errorf("request = %s %v", r.URL.Path, q)
		}
		reply(w, map[string]any{"status": "ok", "total": 3, "results": []any{
			map[string]any{"property": "directorshipDirector", "count": 3, "entities": []any{
				map[string]any{"id": "d1", "schema": "Directorship", "properties": map[string]any{"role": []any{"CEO"}}},
				map[string]any{"id": "c1", "schema": "Company", "properties": map[string]any{"name": []any{"Acme Corp"}}},
			}},
		}})
	})
	h.login()
	stdout, stderr, err := h.run("entity", "expand", "e1", "--property", "directorshipDirector", "--property", "ownershipOwner", "--limit", "50")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PROPERTY", "directorshipDirector", "Directorship", "Acme Corp"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "more connections") {
		t.Errorf("stderr = %q", stderr)
	}

	stdout, _, err = h.run("entity", "expand", "e1", "--property", "directorshipDirector,ownershipOwner", "--limit", "50", "--json", "property,count")
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v: %q", err, stdout)
	}
	if len(got) != 1 || got[0]["count"] != float64(3) || got[0]["entities"] != nil {
		t.Fatalf("got %v", got)
	}
}

func TestEntityTags(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/2/entities/e1/tags" {
			t.Errorf("path = %s", r.URL.Path)
		}
		reply(w, map[string]any{"status": "ok", "total": 2, "results": []any{
			map[string]any{"id": "email:jane@example.org", "field": "emails", "value": "jane@example.org", "count": 4},
			map[string]any{"id": "phone:+4930123", "field": "phones", "value": "+4930123", "count": 1},
		}})
	})
	h.login()
	stdout, _, err := h.run("entity", "tags", "e1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FIELD", "emails", "jane@example.org", "4", "+4930123"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}
