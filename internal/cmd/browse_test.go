package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

func TestBrowseBackendFolders(t *testing.T) {
	var got []url.Values
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query())
		reply(w, map[string]any{"total": 0, "results": []any{}})
	})
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	b := browseBackend{c}
	ctx := t.Context()
	for _, q := range []tui.EntityQuery{
		{CollectionID: "12", TopLevel: true},
		{Parent: "d2"},
		{Parent: "d2", Query: "invoice"},
	} {
		if _, err := b.Entities(ctx, q, 50); err != nil {
			t.Fatal(err)
		}
	}
	checks := []map[string]string{
		{"filter:collection_id": "12", "empty:properties.parent": "true", "filter:schemata": "Document", "offset": "50"},
		{"filter:properties.parent": "d2", "filter:schemata": "Document"},
		{"filter:properties.ancestors": "d2", "q": "invoice", "filter:schemata": "Document"},
	}
	for i, want := range checks {
		for k, v := range want {
			if got[i].Get(k) != v {
				t.Errorf("query %d: %s = %q, want %q (%v)", i, k, got[i].Get(k), v, got[i])
			}
		}
	}
	if got[2].Has("filter:properties.parent") {
		t.Errorf("search in a folder must use ancestors only: %v", got[2])
	}
}

func TestBrowseBackendDownload(t *testing.T) {
	var base string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/2/entities/d1":
			reply(w, map[string]any{"id": "d1", "schema": "Pages",
				"properties": map[string]any{"fileName": []any{"report.pdf"}},
				"links":      map[string]any{"file": base + "/files/d1"}})
		case "/files/d1":
			_, _ = w.Write([]byte("%PDF"))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	base = h.srv.URL
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var progress []string
	res, err := browseBackend{c}.Download(t.Context(), "d1", dir, false, func(r tui.DownloadResult, name string) {
		progress = append(progress, fmt.Sprintf("%d %s", r.Fetched, name))
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "report.pdf"))
	if err != nil || string(b) != "%PDF" || res.Fetched != 1 || strings.Join(progress, ",") != "1 report.pdf" {
		t.Fatalf("file = %q (%v), result = %+v, progress = %q", b, err, res, progress)
	}
}

func TestBrowseBackendPagesAndCSV(t *testing.T) {
	var pagesQuery url.Values
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/2/entities":
			pagesQuery = r.URL.Query()
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"schema": "Page"}}})
		case "/archive/table.csv":
			_, _ = w.Write([]byte("id,name\n1,\"Acme, Inc.\"\n2,Globex\n3,Initech\n"))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	b := browseBackend{c}
	if _, err := b.Pages(t.Context(), "doc1", 50); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"filter:schema": "Page", "filter:properties.document": "doc1", "sort": "properties.index:asc", "offset": "50"} {
		if got := pagesQuery.Get(k); got != want {
			t.Errorf("pages %s = %q, want %q", k, got, want)
		}
	}
	if pagesQuery.Has("filter:schemata") {
		t.Errorf("pages query must not limit to Things: %v", pagesQuery)
	}

	rows, more, err := b.CSV(t.Context(), h.srv.URL+"/archive/table.csv", 2)
	if err != nil || !more || len(rows) != 2 || rows[1][1] != "Acme, Inc." {
		t.Fatalf("rows = %q, more = %v, err = %v", rows, more, err)
	}
	rows, more, err = b.CSV(t.Context(), h.srv.URL+"/archive/table.csv", 10)
	if err != nil || more || len(rows) != 4 {
		t.Fatalf("all rows = %q, more = %v, err = %v", rows, more, err)
	}
}

func TestBrowseBackendAttachmentCounts(t *testing.T) {
	var got url.Values
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		reply(w, map[string]any{"total": 3, "results": []any{}, "facets": map[string]any{
			"properties.parent": map[string]any{"values": []any{map[string]any{"id": "e1", "count": 2}, map[string]any{"id": "e3", "count": 1},
				// The parent of an attachment of an attachment.
				map[string]any{"id": "a9", "count": 4}}},
		}})
	})
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	counts, err := browseBackend{c}.AttachmentCounts(t.Context(), []string{"e1", "e2", "e3"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["e1"] != 2 || counts["e3"] != 1 || counts["e2"] != 0 || len(counts) != 2 {
		t.Fatalf("counts = %v", counts)
	}
	// Filtered by ancestors: a filter on the faceted field would not limit
	// the facet in Aleph.
	if strings.Join(got["filter:properties.ancestors"], ",") != "e1,e2,e3" || got.Has("filter:properties.parent") ||
		got.Get("facet") != "properties.parent" || got.Get("facet_size:properties.parent") != "12" ||
		got.Get("filter:schemata") != "Document" || got.Get("limit") != "0" {
		t.Fatalf("query = %v", got)
	}
}

func TestBrowseBackendThread(t *testing.T) {
	m := func(id, date string, props map[string]any) map[string]any {
		props["date"] = []any{date}
		return map[string]any{"id": id, "schema": "Email", "properties": props}
	}
	// A reply chain without thread topic: e1 <- e2 <- e3, and e2 <- e4.
	byMsgID := map[string]map[string]any{
		"<1@x>": m("e1", "2016-01-01", map[string]any{"messageId": []any{"<1@x>"}}),
		"<2@x>": m("e2", "2016-01-02", map[string]any{"messageId": []any{"<2@x>"}, "inReplyToEmail": []any{"<1@x>"}}),
		"<3@x>": m("e3", "2016-01-03", map[string]any{"messageId": []any{"<3@x>"}, "inReplyToEmail": []any{"<2@x>"}}),
		"<4@x>": m("e4", "2016-01-04", map[string]any{"messageId": []any{"<4@x>"}, "inReplyToEmail": []any{"<2@x>"}}),
	}
	var queries []url.Values
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		queries = append(queries, q)
		var res []any
		switch {
		case q.Has("filter:properties.threadTopic"):
			res = []any{m("t2", "2016-02-02", map[string]any{}), m("t1", "2016-02-01", map[string]any{})}
		case q.Has("filter:properties.messageId"):
			if e, ok := byMsgID[q.Get("filter:properties.messageId")]; ok {
				res = []any{e}
			}
		case q.Has("filter:properties.inReplyToEmail"):
			for _, e := range byMsgID {
				p := e["properties"].(map[string]any)
				if r, ok := p["inReplyToEmail"].([]any); ok && r[0] == q.Get("filter:properties.inReplyToEmail") {
					res = append(res, e)
				}
			}
		}
		reply(w, map[string]any{"total": len(res), "results": res})
	})
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	b := browseBackend{c}
	ids := func(es []api.Object) string {
		var s []string
		for _, e := range es {
			s = append(s, api.ID(e))
		}
		return strings.Join(s, ",")
	}

	// From the middle of the chain the whole thread is found, oldest first.
	e3 := byMsgID["<3@x>"]
	e3["collection"] = map[string]any{"id": "12"}
	thread, err := b.Thread(t.Context(), e3)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(thread); got != "e1,e2,e3,e4" {
		t.Fatalf("thread = %s", got)
	}
	for _, q := range queries {
		if q.Get("filter:collection_id") != "12" || q.Get("filter:schema") != "Email" {
			t.Fatalf("thread query = %v", q)
		}
	}

	// With a thread topic one query is enough.
	queries = nil
	thread, err = b.Thread(t.Context(), m("t1", "2016-02-01", map[string]any{"threadTopic": []any{"Offer"}}))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(thread); got != "t1,t2" || len(queries) != 1 || queries[0].Get("filter:properties.threadTopic") != "Offer" {
		t.Fatalf("thread = %s, queries = %v", got, queries)
	}
}

func TestAttachmentCountsStayWithinRequestLineLimit(t *testing.T) {
	var lines []int
	var seen []string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		// gunicorn rejects request lines over 4094 bytes.
		line := len(r.Method) + 1 + len(r.URL.RequestURI()) + len(" HTTP/1.1")
		lines = append(lines, line)
		if line > 4094 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ids := r.URL.Query()["filter:properties.ancestors"]
		seen = append(seen, ids...)
		var vals []any
		for _, id := range ids {
			vals = append(vals, map[string]any{"id": id, "count": 1})
		}
		reply(w, map[string]any{"total": len(ids), "results": []any{}, "facets": map[string]any{"properties.parent": map[string]any{"values": vals}}})
	})
	c, err := api.New(api.Options{Host: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	// 50 IDs as long as real ones (a hash and a collection signature).
	var ids []string
	for i := range 50 {
		ids = append(ids, fmt.Sprintf("%040x.%040x", i, i))
	}
	counts, err := browseBackend{c}.AttachmentCounts(t.Context(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 50 || len(seen) != 50 || len(lines) < 2 {
		t.Fatalf("counts = %d, ids sent = %d, requests = %d", len(counts), len(seen), len(lines))
	}
	for _, l := range lines {
		if l > 4094 {
			t.Fatalf("request line of %d bytes", l)
		}
	}
}
