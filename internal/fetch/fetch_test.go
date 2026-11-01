package fetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// newServer serves a collection with this layout:
//
//	f1 (file "one.txt"), folder d1 ("Folder") containing f2 ("two.txt"),
//	and a file whose fileName tries to escape the destination.
func newServer(t *testing.T) *api.Client {
	t.Helper()
	var base string
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	file := func(id, name string) map[string]any {
		return map[string]any{
			"id":         id,
			"properties": map[string]any{"fileName": []any{name}},
			"links":      map[string]any{"file": base + "/files/" + id},
		}
	}
	mux.HandleFunc("/api/2/entities", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("filter:schemata") != "Document" {
			t.Errorf("schemata = %q", q.Get("filter:schemata"))
		}
		switch {
		case q.Get("filter:collection_id") == "5" && q.Get("empty:properties.parent") == "true":
			reply(w, map[string]any{"total": 3, "results": []any{
				file("f1", "one.txt"),
				map[string]any{"id": "d1", "properties": map[string]any{"fileName": []any{"Folder"}}},
				file("evil", "../../escaped.txt"),
			}})
		case q.Get("filter:properties.parent") == "d1":
			reply(w, map[string]any{"total": 1, "results": []any{file("f2", "two.txt")}})
		default:
			t.Errorf("unexpected query %v", q)
		}
	})
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("content of " + strings.TrimPrefix(r.URL.Path, "/files/")))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	c, err := api.New(api.Options{Host: srv.URL, Retries: 1, Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCollectionDownloadsTree(t *testing.T) {
	c := newServer(t)
	parent := t.TempDir()
	dest := filepath.Join(parent, "out")
	res, err := Collection(context.Background(), c, api.Object{"id": "5", "label": "Leak"}, Options{Prefix: dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Fetched != 3 || res.Skipped != 0 {
		t.Fatalf("result = %+v", res)
	}
	if got := read(t, filepath.Join(dest, "one.txt")); got != "content of f1" {
		t.Fatalf("one.txt = %q", got)
	}
	if got := read(t, filepath.Join(dest, "Folder", "two.txt")); got != "content of f2" {
		t.Fatalf("two.txt = %q", got)
	}
}

func TestHostileFileNameStaysInsideDestination(t *testing.T) {
	c := newServer(t)
	parent := t.TempDir()
	dest := filepath.Join(parent, "out")
	if _, err := Collection(context.Background(), c, api.Object{"id": "5"}, Options{Prefix: dest}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, "escaped.txt")); err == nil {
		t.Fatal("file escaped the destination directory")
	}
	if _, err := os.Stat(filepath.Join(dest, ".._.._escaped.txt")); err != nil {
		t.Fatalf("sanitized file missing: %v", err)
	}
}

func TestExistingFilesAreKeptUnlessOverwrite(t *testing.T) {
	c := newServer(t)
	dest := t.TempDir()
	keep := filepath.Join(dest, "one.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Collection(context.Background(), c, api.Object{"id": "5"}, Options{Prefix: dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || read(t, keep) != "mine" {
		t.Fatalf("existing file must be kept: %+v %q", res, read(t, keep))
	}
	if _, err := Collection(context.Background(), c, api.Object{"id": "5"}, Options{Prefix: dest, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, keep); got != "content of f1" {
		t.Fatalf("overwrite must replace the file: %q", got)
	}
}

func TestNoTempFilesLeftBehind(t *testing.T) {
	c := newServer(t)
	dest := t.TempDir()
	if _, err := Collection(context.Background(), c, api.Object{"id": "5"}, Options{Prefix: dest}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dest)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".oa-download-") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"a.txt":  "a.txt",
		"../x":   ".._x",
		"a/b\\c": "a_b_c",
		"..":     "id1",
		"":       "id1",
		"  ":     "id1",
	}
	for in, want := range cases {
		if got := safeName(in, "id1"); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := safeName("", ".."); got != "unnamed" {
		t.Errorf("fallback = %q", got)
	}
}

func TestFileNamePicksLongest(t *testing.T) {
	e := api.Object{"id": "x", "properties": map[string]any{"fileName": []any{"a.txt", "report-final.txt", "b"}}}
	if got := fileName(e); got != "report-final.txt" {
		t.Fatalf("got %q", got)
	}
	if got := fileName(api.Object{"id": "x"}); got != "x" {
		t.Fatalf("fallback = %q", got)
	}
}
