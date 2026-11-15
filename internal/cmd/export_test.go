package cmd

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportSearch(t *testing.T) {
	started := false
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/2/collections/5":
			reply(w, map[string]any{"id": "5", "foreign_id": "leak"})
		case r.URL.Path == "/api/2/search/export" && r.Method == "POST":
			q := r.URL.Query()
			if q.Get("q") != "acme corp" || q.Get("filter:schema") != "Company" || q.Get("filter:collection_id") != "5" {
				t.Errorf("query = %v", q)
			}
			started = true
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	h.login()
	_, stderr, err := h.run("export", "search", "acme", "corp", "--schema", "Company", "--collection", "5")
	if err != nil {
		t.Fatal(err)
	}
	if !started || !strings.Contains(stderr, "Export started") {
		t.Fatalf("started=%v stderr=%q", started, stderr)
	}
}

func exportListHandler(t *testing.T, file string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/2/exports":
			reply(w, map[string]any{"total": 3, "results": []any{
				map[string]any{
					"id": "7", "label": "Search: acme", "status": "successful", "file_size": 2048,
					"file_name": "acme.zip", "created_at": "2026-11-01T10:20:30.123", "expires_at": "2026-12-01T10:20:30",
					"links": map[string]any{"download": "http://" + r.Host + "/api/2/archive?token=abc"},
				},
				map[string]any{"id": "8", "label": "Search: pending", "status": "pending"},
				map[string]any{"id": "9", "label": "Search: broken", "status": "failed"},
			}})
		case "/api/2/archive":
			if r.URL.Query().Get("token") != "abc" {
				t.Errorf("token = %q", r.URL.Query().Get("token"))
			}
			_, _ = w.Write([]byte(file))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	}
}

func TestExportListTable(t *testing.T) {
	h := newHarness(t, exportListHandler(t, ""))
	h.login()
	stdout, _, err := h.run("export", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"STATUS", "Search: acme", "successful", "2.0 KiB", "2026-11-01 10:20", "pending"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	stdout, _, err = h.run("export", "list", "--jq", ".[].status")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "successful\npending\nfailed\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestExportDownload(t *testing.T) {
	h := newHarness(t, exportListHandler(t, "zipdata"))
	h.login()
	dir := t.TempDir()

	// Into a directory, with the server's file name.
	if _, _, err := h.run("export", "download", "7", "-o", dir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "acme.zip")
	if b, err := os.ReadFile(target); err != nil || string(b) != "zipdata" {
		t.Fatalf("file = %q err=%v", b, err)
	}

	// An existing file is never replaced without --overwrite.
	if err := os.WriteFile(target, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := h.run("export", "download", "7", "-o", target)
	if err == nil || !strings.Contains(err.Error(), "--overwrite") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "mine" {
		t.Fatalf("file was replaced: %q", b)
	}

	if _, _, err := h.run("export", "download", "7", "-o", target, "--overwrite"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "zipdata" {
		t.Fatalf("file = %q", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestExportDownloadNotReady(t *testing.T) {
	h := newHarness(t, exportListHandler(t, ""))
	h.login()
	dir := t.TempDir()
	for id, want := range map[string]string{"8": "not ready", "9": "failed", "10": "not found"} {
		_, _, err := h.run("export", "download", id, "-o", dir)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}
