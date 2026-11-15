package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestEntitySetView(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/2/entitysets/es1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		reply(w, map[string]any{
			"id": "es1", "type": "list", "label": "Suspects", "summary": "People of interest",
			"writeable": true, "collection": map[string]any{"label": "Leak A"},
		})
	})
	h.login()
	stdout, _, err := h.run("entityset", "view", "es1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Suspects", "type:", "list", "Leak A", "writeable:", "People of interest"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestEntitySetEdit(t *testing.T) {
	var body map[string]any
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/2/entitysets/es1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		body = nil
		_ = json.NewDecoder(r.Body).Decode(&body)
		reply(w, map[string]any{"id": "es1"})
	})
	h.login()
	if _, _, err := h.run("entityset", "edit", "es1", "--summary", ""); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["summary"] != "" {
		t.Fatalf("body = %v", body)
	}
	if _, _, err := h.run("entityset", "edit", "es1", "--label", "New"); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["label"] != "New" {
		t.Fatalf("body = %v", body)
	}
	if _, _, err := h.run("entityset", "edit", "es1"); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := h.run("entityset", "edit", "es1", "--label", ""); err == nil {
		t.Fatal("empty label must fail")
	}
}

func TestEntitySetAddRemove(t *testing.T) {
	var got []string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/2/entitysets/es1/items" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body["entity_id"].(string)+"="+body["judgement"].(string))
		reply(w, map[string]any{"id": "es1$" + body["entity_id"].(string)})
	})
	h.login()
	if _, _, err := h.run("entityset", "add", "es1", "e1", "e2"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run("entityset", "add", "es1", "e3", "--judgement", "unsure"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run("entityset", "remove", "es1", "e1"); err != nil {
		t.Fatal(err)
	}
	want := "e1=positive e2=positive e3=unsure e1=no_judgement"
	if s := strings.Join(got, " "); s != want {
		t.Fatalf("got %q, want %q", s, want)
	}
	if _, _, err := h.run("entityset", "add", "es1", "e1", "--judgement", "no_judgement"); err == nil {
		t.Fatal("invalid judgement must fail")
	}
}

func TestEntitySetAddStopsOnError(t *testing.T) {
	calls := 0
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
		reply(w, map[string]any{"message": "not found"})
	})
	h.login()
	_, _, err := h.run("entityset", "add", "es1", "missing", "e2")
	if err == nil || !strings.Contains(err.Error(), "entity missing") {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestEntitySetWrite(t *testing.T) {
	var schemas []string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/2/entitysets/es1/entities" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		schemas = append(schemas, body["schema"].(string))
		id, _ := body["id"].(string)
		if id == "" {
			id = "new"
		}
		reply(w, map[string]any{"id": id})
	})
	h.login()
	h.stdin = `{"id":"p1","schema":"Person"}` + "\n\n" + `{"schema":"Company"}` + "\n"
	stdout, _, err := h.run("entityset", "write", "es1")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "p1\nnew\n" || strings.Join(schemas, ",") != "Person,Company" {
		t.Fatalf("stdout = %q, schemas = %v", stdout, schemas)
	}
	h.stdin = "\n"
	if _, _, err := h.run("entityset", "write", "es1"); err == nil {
		t.Fatal("empty input must fail")
	}
}
