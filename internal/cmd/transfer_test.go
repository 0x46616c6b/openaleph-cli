package cmd

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCollectionUploadCreatesCollectionAndUploads(t *testing.T) {
	var mu sync.Mutex
	var uploads []string
	created := false
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/2/collections" && r.Method == "GET":
			reply(w, map[string]any{"total": 0, "results": []any{}})
		case r.URL.Path == "/api/2/collections" && r.Method == "POST":
			created = true
			reply(w, map[string]any{"id": "5", "foreign_id": "docs"})
		case r.URL.Path == "/api/2/collections/5/ingest":
			_ = r.ParseMultipartForm(1 << 20)
			mu.Lock()
			uploads = append(uploads, r.FormValue("meta"))
			mu.Unlock()
			reply(w, map[string]any{"id": "x" + string(rune('a'+len(uploads)))})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	h.login()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := h.run("collection", "upload", "docs", dir, "-l", "de")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if !created || len(uploads) != 1 || !strings.Contains(uploads[0], `"foreign_id":"a.txt"`) {
		t.Fatalf("created=%v uploads=%v", created, uploads)
	}
	if !strings.Contains(stderr, "| INFO") || !strings.Contains(stderr, "Crawldir complete.") {
		t.Fatalf("stderr = %q", stderr)
	}

	// A second run without --resume must refuse instead of deleting the state.
	_, _, err = h.run("collection", "upload", "docs", dir)
	if err == nil || !strings.Contains(err.Error(), "--resume") {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectionDownload(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/2/collections/5":
			reply(w, map[string]any{"id": "5", "label": "Docs"})
		case "/api/2/entities":
			reply(w, map[string]any{"total": 1, "results": []any{map[string]any{
				"id":         "f1",
				"properties": map[string]any{"fileName": []any{"hello.txt"}},
				"links":      map[string]any{"file": "http://" + r.Host + "/files/f1"},
			}}})
		case "/files/f1":
			_, _ = w.Write([]byte("hello"))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	h.login()
	out := t.TempDir()
	if _, _, err := h.run("collection", "download", "5", "-o", out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "hello.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("file = %q err=%v", b, err)
	}
}
