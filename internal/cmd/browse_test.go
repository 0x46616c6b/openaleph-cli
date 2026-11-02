package cmd

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/tui"
)

func TestBrowseNeedsTerminal(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("browse without a terminal must not call the API")
	})
	h.login()
	_, _, err := h.run("browse")
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("err = %v", err)
	}
}

func TestBrowseBackendPages(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/2/entities":
			if q.Get("q") != "acme" || q.Get("filter:collection_id") != "12" ||
				q.Get("offset") != "50" || q.Get("limit") != "50" || q.Get("filter:schemata") != "Thing" {
				t.Errorf("entities query = %v", q)
			}
			reply(w, map[string]any{"total": 120, "total_type": "gte", "next": "http://next", "results": []any{
				map[string]any{"id": "e1", "caption": "Acme"},
			}})
		case "/api/2/status":
			reply(w, map[string]any{"total": 2, "results": []any{
				map[string]any{"collection_id": 7, "todo": 3},
				"junk",
			}})
		case "/api/2/collections":
			if q.Get("offset") != "0" || q.Get("exclude:category") != "casefile" || q.Has("filter:category") {
				t.Errorf("collections query = %v", q)
			}
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "12"}}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	b := browseBackend{c}

	p, err := b.Entities(t.Context(), tui.EntityQuery{Query: "acme", CollectionID: "12"}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 120 || !p.TotalAtLeast || !p.More || len(p.Items) != 1 || p.Items[0]["id"] != "e1" {
		t.Fatalf("entities page = %+v", p)
	}
	p, err = b.Collections(t.Context(), "", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 || p.More {
		t.Fatalf("collections page = %+v", p)
	}
	rows, err := b.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["todo"] != 3.0 {
		t.Fatalf("status rows = %v", rows)
	}
}

func TestOpenBrowserRejectsOtherSchemes(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "-x", "calc.exe"} {
		if err := openBrowser(u); err == nil || !strings.Contains(err.Error(), "not an http(s) URL") {
			t.Errorf("openBrowser(%q) err = %v", u, err)
		}
	}
}

func TestBrowseBackendCollectionAdmin(t *testing.T) {
	type req struct {
		method, path, query string
		body                map[string]any
	}
	var got []req
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, req{r.Method, r.URL.Path, r.URL.RawQuery, body})
		switch {
		case r.Method == "GET" && r.URL.Query().Get("filter:foreign_id") == "taken":
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "5"}}})
		case r.Method == "GET":
			reply(w, map[string]any{"total": 0, "results": []any{}})
		default:
			reply(w, map[string]any{"id": "7", "label": "New"})
		}
	})
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	b := browseBackend{c}
	ctx := t.Context()

	if _, err := b.CreateCollection(ctx, api.Object{"foreign_id": "taken"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("create taken: err = %v", err)
	}
	got = nil
	if _, err := b.CreateCollection(ctx, api.Object{"foreign_id": "fresh", "label": "New"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].method != "POST" || got[1].path != "/api/2/collections" || got[1].body["label"] != "New" {
		t.Fatalf("create requests = %+v", got)
	}

	got = nil
	coll := api.Object{"id": "7", "label": "Old", "category": "leak", "count": 3.0, "publisher": "ACME"}
	if _, err := b.UpdateCollection(ctx, coll, api.Object{"label": "New"}); err != nil {
		t.Fatal(err)
	}
	u := got[0]
	if u.method != "PUT" || u.path != "/api/2/collections/7" || u.query != "sync=true" ||
		u.body["label"] != "New" || u.body["category"] != "leak" || u.body["publisher"] != "ACME" || u.body["count"] != nil {
		t.Fatalf("update request = %+v", u)
	}

	got = nil
	if err := b.DeleteCollection(ctx, "7"); err != nil {
		t.Fatal(err)
	}
	if err := b.ReindexCollection(ctx, "7", true); err != nil {
		t.Fatal(err)
	}
	if err := b.ReingestCollection(ctx, "7", true); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DELETE /api/2/collections/7 sync=true",
		"POST /api/2/collections/7/reindex flush=true&sync=false",
		"POST /api/2/collections/7/reingest index=true",
	}
	for i, w := range want {
		if g := got[i].method + " " + got[i].path + " " + got[i].query; g != w {
			t.Errorf("request %d = %q, want %q", i, g, w)
		}
	}
}

func TestBrowseBackendInvestigations(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("filter:category") != "casefile" || q.Has("exclude:category") || q.Get("q") != "case" {
			t.Errorf("investigations query = %v", q)
		}
		reply(w, map[string]any{"total": 0, "results": []any{}})
	})
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (browseBackend{c}).Collections(t.Context(), "case", true, 0); err != nil {
		t.Fatal(err)
	}
}

func TestBrowseBackendSchemaCounts(t *testing.T) {
	var got url.Values
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		reply(w, map[string]any{"total": 5, "results": []any{}, "facets": map[string]any{
			"schema": map[string]any{"values": []any{map[string]any{"id": "Person", "label": "People", "count": 5}}},
		}})
	})
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	b := browseBackend{c}
	vals, err := b.SchemaCounts(t.Context(), tui.EntityQuery{Query: "acme", CollectionID: "12", Schemas: []string{"Address"}})
	if err != nil {
		t.Fatal(err)
	}
	// Counts cover all schemas: no schema filter, no Thing default.
	if got.Get("facet") != "schema" || got.Get("limit") != "0" || got.Get("filter:collection_id") != "12" ||
		got.Has("filter:schema") || got.Has("filter:schemata") {
		t.Fatalf("query = %v", got)
	}
	if len(vals) != 1 || vals[0].ID != "Person" || vals[0].Count != 5 {
		t.Fatalf("values = %+v", vals)
	}

	if _, err := b.Entities(t.Context(), tui.EntityQuery{Schemas: []string{"Person", "Address"}}, 0); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got["filter:schema"], ",") != "Person,Address" || got.Has("filter:schemata") {
		t.Fatalf("entities query = %v", got)
	}
}

func TestPageParamsRespectsServerLimit(t *testing.T) {
	for offset, want := range map[int]string{0: "50", 9950: "49", 9999: "0", 12000: "0"} {
		if got := pageParams(offset).Get("limit"); got != want {
			t.Errorf("pageParams(%d) limit = %s, want %s", offset, got, want)
		}
	}
}
