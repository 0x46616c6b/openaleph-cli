package api

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Options{
		Host:    srv.URL,
		APIKey:  "secret",
		Retries: 3,
		Sleep:   func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestNewRequiresHost(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected error for empty host")
	}
	if _, err := New(Options{Host: "aleph.example.org"}); err == nil {
		t.Fatal("expected error for host without scheme")
	}
}

func TestHeaders(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "ApiKey secret" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("X-Aleph-Session") == "" {
			t.Error("missing session header")
		}
		if r.URL.Path != "/api/2/status" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(w, Object{"total": 0})
	}))
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAPIKeyNotSentToOtherHost(t *testing.T) {
	var got atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("Authorization"))
	}))
	defer other.Close()
	c, _ := newTestClient(t, http.NotFoundHandler())
	resp, err := c.Do(context.Background(), "GET", other.URL+"/file", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if v := got.Load(); v != "" {
		t.Fatalf("api key leaked to other host: %v", v)
	}
}

func TestErrorMessageFromJSON(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeJSON(w, Object{"status": "error", "message": "Not authorized."})
	}))
	_, err := c.Status(context.Background())
	if err == nil || err.Error() != "Not authorized." {
		t.Fatalf("err = %v", err)
	}
	if StatusCode(err) != 403 || IsTransient(err) {
		t.Fatalf("status=%d transient=%v", StatusCode(err), IsTransient(err))
	}
}

func TestRetryTransient(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "boom", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, Object{"id": "1"})
	}))
	out, err := c.WriteEntity(context.Background(), "5", Object{"schema": "Person"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if out["id"] != "1" || calls.Load() != 3 {
		t.Fatalf("out=%v calls=%d", out, calls.Load())
	}
}

func TestRetryStopsOnClientError(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	if _, err := c.WriteEntity(context.Background(), "5", Object{}, ""); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestRetryGivesUp(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	if _, err := c.WriteEntity(context.Background(), "5", Object{}, ""); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 4 { // first attempt + 3 retries
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestPaging(t *testing.T) {
	var srvURL string
	c, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("offset") {
		case "":
			writeJSON(w, Object{"total": 3, "limit": 2, "offset": 0, "results": []Object{{"id": "a"}, {"id": "b"}},
				"next": srvURL + "/api/2/collections?offset=2&limit=2"})
		case "2":
			writeJSON(w, Object{"total": 3, "limit": 2, "offset": 2, "results": []Object{{"id": "c"}}})
		}
	}))
	srvURL = srv.URL
	rs, err := c.ListCollections(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	items, err := rs.All(context.Background())
	if err != nil || len(items) != 3 || items[2]["id"] != "c" || rs.Total() != 3 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestSearchDefaultsToThing(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "foo" || q.Get("filter:schemata") != "Thing" {
			t.Errorf("query = %v", q)
		}
		writeJSON(w, Object{"results": []Object{{"id": "e1"}}, "total": 1})
	}))
	rs, err := c.Search(context.Background(), SearchOptions{Query: "foo"})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := rs.All(context.Background())
	props := items[0]["properties"].(map[string]any)
	if urls := props["alephUrl"].([]any); len(urls) != 1 || !strings.HasSuffix(urls[0].(string), "/api/2/entities/e1") {
		t.Fatalf("alephUrl = %v", props["alephUrl"])
	}
}

func TestResolveCollection(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/2/collections/42":
			writeJSON(w, Object{"id": "42"})
		case r.URL.Query().Get("filter:foreign_id") == "known":
			writeJSON(w, Object{"results": []Object{{"id": "7"}}, "total": 1})
		default:
			writeJSON(w, Object{"results": []Object{}, "total": 0})
		}
	}))
	ctx := context.Background()
	if got, err := c.ResolveCollection(ctx, "42"); err != nil || got["id"] != "42" {
		t.Fatalf("by id: %v %v", got, err)
	}
	if got, err := c.ResolveCollection(ctx, "known"); err != nil || got["id"] != "7" {
		t.Fatalf("by foreign id: %v %v", got, err)
	}
	if _, err := c.ResolveCollection(ctx, "missing"); err == nil {
		t.Fatal("expected error for unknown collection")
	}
}

func TestFlushKeepsMetadata(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != "DELETE" || q.Get("keep_metadata") != "true" || q.Get("sync") != "true" {
			t.Errorf("%s %s", r.Method, r.URL)
		}
	}))
	if err := c.FlushCollection(context.Background(), "9", true); err != nil {
		t.Fatal(err)
	}
}

func TestStreamEntities(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/2/collections/3/_stream" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query()["schema"]; len(got) != 2 {
			t.Errorf("schema = %v", got)
		}
		_, _ = io.WriteString(w, `{"id":"a","schema":"Person"}`+"\n"+`{"id":"b","schema":"Company"}`)
	}))
	var ids []string
	err := c.StreamEntities(context.Background(), StreamOptions{
		Collection: Object{"id": float64(3)},
		Schemas:    []string{"Person", "Company"},
	}, func(e Object) error {
		ids = append(ids, e["id"].(string))
		return nil
	})
	if err != nil || strings.Join(ids, ",") != "a,b" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
}

func seq(entities ...Object) iter.Seq2[Object, error] {
	return func(yield func(Object, error) bool) {
		for _, e := range entities {
			if !yield(e, nil) {
				return
			}
		}
	}
}

func TestWriteEntitiesChunks(t *testing.T) {
	var sizes []int
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var chunk []Object
		_ = json.NewDecoder(r.Body).Decode(&chunk)
		sizes = append(sizes, len(chunk))
		q := r.URL.Query()
		if q.Get("safe") != "false" || q.Get("clean") != "false" || q.Get("entityset_id") != "es1" {
			t.Errorf("query = %v", q)
		}
	}))
	err := c.WriteEntities(context.Background(), "1", seq(Object{"id": "1"}, Object{"id": "2"}, Object{"id": "3"}),
		BulkOptions{ChunkSize: 2, Unsafe: true, Cleaned: true, EntitySetID: "es1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 2 || sizes[0] != 2 || sizes[1] != 1 {
		t.Fatalf("sizes = %v", sizes)
	}
}

func TestWriteEntitiesForce(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	var logged int
	err := c.WriteEntities(context.Background(), "1", seq(Object{"id": "1"}),
		BulkOptions{Force: true, OnError: func(error) { logged++ }})
	if err != nil || logged != 1 {
		t.Fatalf("err=%v logged=%d", err, logged)
	}
}

func TestIngestUploadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		var meta Object
		_ = json.Unmarshal([]byte(r.FormValue("meta")), &meta)
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		body, _ := io.ReadAll(f)
		if string(body) != "hello" || hdr.Filename != "doc.txt" || meta["foreign_id"] != "doc.txt" {
			t.Errorf("body=%q name=%q meta=%v", body, hdr.Filename, meta)
		}
		if r.URL.Query().Get("index") != "false" {
			t.Errorf("index = %q", r.URL.Query().Get("index"))
		}
		writeJSON(w, Object{"id": "99"})
	}))
	out, err := c.IngestUpload(context.Background(), "1", path, Object{"foreign_id": "doc.txt"}, false, false)
	if err != nil || out["id"] != "99" {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestIngestUploadFolder(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("meta") == "" || r.FormValue("meta") == "null" {
			t.Errorf("meta = %q", r.FormValue("meta"))
		}
		writeJSON(w, Object{"id": "folder"})
	}))
	out, err := c.IngestUpload(context.Background(), "1", t.TempDir(), Object{"foreign_id": "sub", "file_name": "sub"}, false, true)
	if err != nil || out["id"] != "folder" {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestCreateEntitySetSendsJSON(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var body Object
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["type"] != "list" || body["collection_id"] != "4" || body["label"] != "L" {
			t.Errorf("body = %v", body)
		}
		writeJSON(w, Object{"id": "es"})
	}))
	out, err := c.CreateEntitySet(context.Background(), "4", "list", "L", "")
	if err != nil || out["id"] != "es" {
		t.Fatalf("out=%v err=%v", out, err)
	}
}
