package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEntityProcessing(t *testing.T) {
	var seen []string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, strings.TrimSpace(r.Method+" "+r.URL.Path+" "+string(b)))
		w.WriteHeader(http.StatusAccepted)
	})
	h.login()
	for _, args := range [][]string{
		{"entity", "reingest", "e1", "e2"},
		{"entity", "analyze", "e1"},
		{"entity", "transcribe", "e3"},
		{"entity", "translate", "e1"},
		{"entity", "translate", "e2", "--source-language", "de"},
	} {
		if _, _, err := h.run(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	want := []string{
		"POST /api/2/entities/e1/ingest",
		"POST /api/2/entities/e2/ingest",
		"POST /api/2/entities/e1/analyze",
		"POST /api/2/entities/e3/transcribe",
		"POST /api/2/entities/e1/translate",
		"POST /api/2/entities/e2/translate",
	}
	if len(seen) != len(want) {
		t.Fatalf("seen = %v", seen)
	}
	for i, w := range want {
		if !strings.HasPrefix(seen[i], w) {
			t.Errorf("request %d = %q, want %q", i, seen[i], w)
		}
	}
	if seen[4] != want[4] {
		t.Errorf("translate without language sent a body: %q", seen[4])
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(seen[5], want[5]+" ")), &body); err != nil || body["source_language"] != "de" {
		t.Errorf("translate body = %q (%v)", seen[5], err)
	}
}

func TestEntityProcessingStopsAtError(t *testing.T) {
	var seen []string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		if strings.Contains(r.URL.Path, "/bad/") {
			w.WriteHeader(http.StatusBadRequest)
			reply(w, map[string]any{"status": "error", "message": "Entity is not eligible for transcription"})
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	h.login()
	_, _, err := h.run("entity", "transcribe", "ok", "bad", "never")
	if err == nil || !strings.Contains(err.Error(), "entity bad") || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(seen, ",") != "/api/2/entities/ok/transcribe,/api/2/entities/bad/transcribe" {
		t.Fatalf("seen = %v", seen)
	}
}

func TestEntityProcessingNeedsID(t *testing.T) {
	h := newHarness(t, func(http.ResponseWriter, *http.Request) {})
	h.login()
	if _, _, err := h.run("entity", "analyze"); err == nil {
		t.Fatal("analyze without ID must fail")
	}
}
