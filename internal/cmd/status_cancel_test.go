package cmd

import (
	"net/http"
	"strings"
	"testing"
)

func TestStatusCancel(t *testing.T) {
	var seen []string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/2/collections/5/status" && r.Method == "DELETE" {
			seen = append(seen, r.Method+" "+r.URL.RequestURI())
			w.WriteHeader(http.StatusNoContent)
			return
		}
		collectionHandler(t, &seen)(w, r)
	})
	h.login()
	_, stderr, err := h.run("status", "cancel", "leak_a", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, "|") != "DELETE /api/2/collections/5/status" {
		t.Fatalf("seen = %v", seen)
	}
	if !strings.Contains(stderr, "leak_a") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestStatusCancelForbidden(t *testing.T) {
	var seen []string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/2/collections/5/status" && r.Method == "DELETE" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"status": "error", "message": "Forbidden"}`))
			return
		}
		collectionHandler(t, &seen)(w, r)
	})
	h.login()
	if _, _, err := h.run("status", "cancel", "5", "--yes"); err == nil {
		t.Fatal("expected an error for a forbidden cancel")
	}
}
